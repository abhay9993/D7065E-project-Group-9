package websocket

import (
	"context"
	"encoding/json"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/eislab-cps/buildingsim/pkg/store"
	ws "github.com/gorilla/websocket"
)

type Message struct {
	Type    string      `json:"type"`
	Data    interface{} `json:"data,omitempty"`
	Version int64       `json:"version,omitempty"`
}

type client struct {
	conn      *ws.Conn
	sessionID string
	send      chan []byte
}

type Hub struct {
	mu       sync.RWMutex
	clients  map[*client]bool
	sessions map[string]map[*client]bool // session id -> clients
	store    *store.MemoryStore
}

func Upgrader() ws.Upgrader {
	return ws.Upgrader{
		CheckOrigin: IsAllowedOrigin,
	}
}

// IsAllowedOrigin permits the same origin and loopback-to-loopback browser
// connections. BuildSim is a laptop-local service; unrelated web sites must
// not be able to open a socket and mutate its state.
func IsAllowedOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true // command-line and non-browser clients do not send Origin
	}
	parsed, err := url.Parse(origin)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return false
	}
	if strings.EqualFold(parsed.Host, r.Host) {
		return true
	}
	return isLoopbackHost(parsed.Hostname()) && isLoopbackHost(requestHostname(r.Host))
}

func requestHostname(hostport string) string {
	if host, _, err := net.SplitHostPort(hostport); err == nil {
		return host
	}
	return strings.Trim(hostport, "[]")
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	return ip != nil && ip.IsLoopback()
}

func NewHub(store *store.MemoryStore) *Hub {
	return &Hub{
		clients:  make(map[*client]bool),
		sessions: make(map[string]map[*client]bool),
		store:    store,
	}
}

func (h *Hub) HandleConnection(conn *ws.Conn, sessionID string) {
	c := &client{
		conn:      conn,
		sessionID: sessionID,
		send:      make(chan []byte, 256),
	}

	h.mu.Lock()
	h.clients[c] = true
	if h.sessions[sessionID] == nil {
		h.sessions[sessionID] = make(map[*client]bool)
	}
	h.sessions[sessionID][c] = true
	// Queue the current session snapshot before broadcasts can acquire the hub
	// lock. This prevents a newly opened viewer from missing changes made just
	// before it connected, while preserving message order for concurrent edits.
	if session, ok := h.store.GetSession(sessionID); ok {
		initial := []Message{
			{Type: "viewport", Data: session.Viewport, Version: session.Version},
			{Type: "highlights", Data: session.Highlights, Version: session.Version},
			{Type: "occupancy", Data: session.Occupancy, Version: session.Version},
			{Type: "coverage", Data: session.Coverage, Version: session.Version},
		}
		if session.Route != nil {
			initial = append(initial, Message{Type: "route", Data: session.Route, Version: session.Version})
		}
		for _, message := range initial {
			if data, err := json.Marshal(message); err == nil {
				c.send <- data
			}
		}
	}
	h.mu.Unlock()

	h.store.TouchSession(sessionID)

	go h.writePump(c)
	h.readPump(c)
}

func (h *Hub) readPump(c *client) {
	defer func() {
		h.removeClient(c)
		c.conn.Close()
	}()

	c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		h.store.TouchSession(c.sessionID)
		return nil
	})

	for {
		_, _, err := c.conn.ReadMessage()
		if err != nil {
			break
		}
		h.store.TouchSession(c.sessionID)
	}
}

func (h *Hub) writePump(c *client) {
	ticker := time.NewTicker(30 * time.Second)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()

	for {
		select {
		case msg, ok := <-c.send:
			if !ok {
				c.conn.WriteMessage(ws.CloseMessage, []byte{})
				return
			}
			c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.conn.WriteMessage(ws.TextMessage, msg); err != nil {
				return
			}
		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.conn.WriteMessage(ws.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

func (h *Hub) removeClient(c *client) {
	h.mu.Lock()
	defer h.mu.Unlock()

	delete(h.clients, c)
	if clients, ok := h.sessions[c.sessionID]; ok {
		delete(clients, c)
		if len(clients) == 0 {
			delete(h.sessions, c.sessionID)
		}
	}
	close(c.send)
}

func (h *Hub) SendToSession(sessionID string, msg Message) {
	data, err := json.Marshal(msg)
	if err != nil {
		log.Printf("Failed to marshal WS message: %v", err)
		return
	}

	h.mu.RLock()
	defer h.mu.RUnlock()
	for c := range h.sessions[sessionID] {
		select {
		case c.send <- data:
		default:
			// Client buffer full, skip
		}
	}
}

func (h *Hub) BroadcastToAll(msg Message) {
	data, err := json.Marshal(msg)
	if err != nil {
		log.Printf("Failed to marshal WS message: %v", err)
		return
	}

	h.mu.RLock()
	defer h.mu.RUnlock()

	for c := range h.clients {
		select {
		case c.send <- data:
		default:
		}
	}
}

func (h *Hub) StartPurger(ctx context.Context, interval time.Duration, maxAge time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				purged := h.store.PurgeInactiveSessions(maxAge)
				for _, id := range purged {
					h.mu.Lock()
					if clients, ok := h.sessions[id]; ok {
						for c := range clients {
							c.conn.Close()
						}
						delete(h.sessions, id)
					}
					h.mu.Unlock()
					log.Printf("Purged inactive session: %s", id)
				}
			}
		}
	}()
}
