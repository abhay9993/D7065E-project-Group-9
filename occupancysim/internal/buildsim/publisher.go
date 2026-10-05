package buildsim

import (
	"context"
	"encoding/json"
	"hash/fnv"
	"log"
	"sync"
	"time"

	"github.com/eislab-cps/D7065E/occupancysim/internal/sim"
)

// Status describes the connection to BuildSim for the UI.
type Status struct {
	URL           string    `json:"url"`
	PublicURL     string    `json:"public_url"`
	Connected     bool      `json:"connected"`
	LastError     string    `json:"last_error,omitempty"`
	LastPublish   time.Time `json:"last_publish"`
	LastOccupancy time.Time `json:"last_occupancy"`
	Entities      int       `json:"entities"`
	Rooms         int       `json:"rooms"`
}

// occupancyRefresh is how long room occupancy may go unwritten while nothing
// changes. It bounds how stale BuildSim can be after a restart we did not
// notice, and costs one write per half minute.
const occupancyRefresh = 30 * time.Second

// Publisher pushes the simulation state to BuildSim on a fixed real-time
// interval. Both endpoints are complete snapshots, so a missed interval needs
// no catch-up: the next publish carries the full current state.
//
// Entities are written on every interval, because their positions change on
// every step and the viewer interpolates between two writes. Room occupancy is
// written far less often: it only changes when somebody enters or leaves a
// room, and BuildSim rebuilds every room sprite of the floor on each write, so
// repeating the same document once a second is load on both sides for nothing.
type Publisher struct {
	client    *Client
	sim       *sim.Simulation
	publicURL string

	mu       sync.Mutex
	status   Status
	failures int

	// owned by the publishing goroutine
	occupancySum   uint64
	occupancyAt    time.Time
	occupancyStale bool // a failed write may have left BuildSim behind
}

func NewPublisher(client *Client, simulation *sim.Simulation, publicURL string) *Publisher {
	return &Publisher{client: client, sim: simulation, publicURL: publicURL, status: Status{URL: client.BaseURL, PublicURL: publicURL}}
}

func (p *Publisher) Status() Status {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.status
}

// Run publishes until the context ends, then clears both collections so the
// viewer does not keep showing a stopped simulation.
func (p *Publisher) Run(ctx context.Context) {
	for {
		interval := time.Duration(p.sim.Parameters().PublishIntervalMs) * time.Millisecond
		select {
		case <-ctx.Done():
			clearCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			_ = p.client.PutEntities(clearCtx, nil)
			_ = p.client.PutOccupancy(clearCtx, nil)
			return
		case <-time.After(interval):
			p.PublishOnce(ctx)
		}
	}
}

// PublishOnce sends the current snapshot and records the outcome. It is called
// from one goroutine at a time.
func (p *Publisher) PublishOnce(ctx context.Context) {
	entities, occupancy := p.sim.Publishable()
	err := p.client.PutEntities(ctx, entities)
	if err == nil {
		err = p.publishOccupancy(ctx, occupancy)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if err != nil {
		p.occupancyStale = true
		p.failures++
		if p.failures == 1 || p.failures%30 == 0 {
			log.Printf("publish to BuildSim failed (%d times): %v", p.failures, err)
		}
		p.status.Connected = false
		p.status.LastError = err.Error()
		return
	}
	if p.failures > 0 {
		log.Printf("publishing to BuildSim resumed after %d failures", p.failures)
	}
	p.failures = 0
	p.status = Status{
		URL: p.client.BaseURL, PublicURL: p.publicURL, Connected: true,
		LastPublish: time.Now(), LastOccupancy: p.occupancyAt,
		Entities: len(entities), Rooms: len(occupancy),
	}
}

// publishOccupancy writes room occupancy when it has something new to say: at
// most once per occupancy interval, and then only if the document differs from
// the one BuildSim already has, if a write failed since, or if the last write
// is older than occupancyRefresh.
func (p *Publisher) publishOccupancy(ctx context.Context, occupancy map[string]sim.RoomOccupancy) error {
	if occupancy == nil {
		occupancy = map[string]sim.RoomOccupancy{}
	}
	interval := time.Duration(p.sim.Parameters().OccupancyIntervalMs) * time.Millisecond
	since := time.Since(p.occupancyAt)
	if !p.occupancyAt.IsZero() && since < interval {
		return nil
	}
	encoded, err := json.Marshal(occupancy)
	if err != nil {
		return err
	}
	sum := fnv.New64a()
	_, _ = sum.Write(encoded)
	digest := sum.Sum64()
	unchanged := !p.occupancyAt.IsZero() && digest == p.occupancySum && !p.occupancyStale
	if unchanged && since < occupancyRefresh {
		return nil
	}
	if err := p.client.PutOccupancyEncoded(ctx, encoded); err != nil {
		return err
	}
	p.occupancySum, p.occupancyAt, p.occupancyStale = digest, time.Now(), false
	return nil
}
