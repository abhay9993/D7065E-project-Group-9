package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/eislab-cps/buildingsim/pkg/graph"
	"github.com/eislab-cps/buildingsim/pkg/model"
	"github.com/eislab-cps/buildingsim/pkg/server/handlers"
	"github.com/eislab-cps/buildingsim/pkg/server/websocket"
	"github.com/eislab-cps/buildingsim/pkg/store"
	"github.com/gin-gonic/gin"
)

const (
	UIClassic = "classic"
	UIModern  = "modern"
)

type Server struct {
	Port          int
	Host          string
	UI            string
	EditMode      bool
	EditOutputDir string
	LogFile       string

	store  *store.MemoryStore
	hub    *websocket.Hub
	dataFS fs.FS
	webFS  fs.FS

	loadOnce sync.Once
	loadErr  error
}

func New(port int, dataFS fs.FS, webFS fs.FS, editMode bool) *Server {
	memory := store.NewMemoryStore()
	return &Server{
		Port:     port,
		Host:     "127.0.0.1",
		UI:       UIModern,
		EditMode: editMode,
		store:    memory,
		hub:      websocket.NewHub(memory),
		dataFS:   dataFS,
		webFS:    webFS,
	}
}

func (s *Server) Address() string {
	host := strings.TrimSpace(s.Host)
	if host == "" {
		host = "127.0.0.1"
	}
	return net.JoinHostPort(host, strconv.Itoa(s.Port))
}

func (s *Server) loadBuildingData() error {
	building := model.Building{
		Name: "A-Building (LTU)",
		Levels: []model.Level{
			{ID: "level0", Label: "Floor 0"},
			{ID: "level1", Label: "Floor 1"},
			{ID: "level2", Label: "Floor 2"},
		},
	}
	s.store.SetBuilding(building)

	for _, level := range building.Levels {
		filename := fmt.Sprintf("data/abuilding/%s/floorplan_data.json", level.ID)
		data, err := fs.ReadFile(s.dataFS, filename)
		if err != nil {
			return fmt.Errorf("read %s: %w", filename, err)
		}
		var floor model.FloorData
		if err := json.Unmarshal(data, &floor); err != nil {
			return fmt.Errorf("parse %s: %w", filename, err)
		}
		s.store.SetFloorData(level.ID, &floor)
		log.Printf("Loaded %s: %d rooms, %d walls", level.ID, len(floor.Rooms), len(floor.Walls))
	}

	edgeFile := "data/abuilding/cross_floor_edges.json"
	edgeData, err := fs.ReadFile(s.dataFS, edgeFile)
	if err != nil {
		log.Printf("No cross-floor edges: %v", err)
	} else {
		var edges []model.CrossFloorEdge
		if err := json.Unmarshal(edgeData, &edges); err != nil {
			return fmt.Errorf("parse %s: %w", edgeFile, err)
		}
		s.store.SetCrossFloorEdges(edges)
		log.Printf("Loaded %d cross-floor edges", len(edges))
	}

	levelOrder := make([]string, 0, len(building.Levels))
	for _, level := range building.Levels {
		levelOrder = append(levelOrder, level.ID)
	}
	multiFloor := graph.BuildMultiFloorGraph(s.store.GetFloors(), levelOrder, s.store.GetCrossFloorEdges())
	s.store.SetMultiFloorGraph(multiFloor)
	log.Printf("Built multi-floor walkable graph: %d nodes, %d edges", len(multiFloor.Nodes), len(multiFloor.Edges))
	return nil
}

// Router constructs the complete HTTP handler. It is exposed for integration
// tests and for embedding BuildSim in another local Go program.
func (s *Server) Router() (*gin.Engine, error) {
	if s.Port < 0 || s.Port > 65535 {
		return nil, fmt.Errorf("invalid port %d", s.Port)
	}
	s.UI = strings.ToLower(strings.TrimSpace(s.UI))
	if s.UI == "" {
		s.UI = UIModern
	}
	if s.UI != UIClassic && s.UI != UIModern {
		return nil, fmt.Errorf("unknown UI %q (use %q or %q)", s.UI, UIClassic, UIModern)
	}

	s.loadOnce.Do(func() { s.loadErr = s.loadBuildingData() })
	if s.loadErr != nil {
		return nil, fmt.Errorf("load building data: %w", s.loadErr)
	}

	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	router.Use(gin.Logger(), gin.Recovery(), localCORSMiddleware())
	router.GET("/healthz", func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	api := router.Group("/api")
	{
		api.GET("/config", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{
				"edit_mode":       s.EditMode,
				"edit_persistent": s.EditMode && s.EditOutputDir != "",
				"ui":              s.UI,
				"host":            defaultHost(s.Host),
			})
		})

		buildingHandlers := &handlers.BuildingHandlers{Store: s.store}
		api.GET("/building", buildingHandlers.GetBuilding)
		api.GET("/building/floors/:level", buildingHandlers.GetFloor)
		api.GET("/building/cross-floor-edges", buildingHandlers.GetCrossFloorEdges)

		graphHandlers := &handlers.GraphHandlers{Store: s.store}
		api.GET("/graph", graphHandlers.GetGraph)
		api.GET("/graph/route", graphHandlers.GetRoute)

		equipmentHandlers := &handlers.EquipmentHandlers{Store: s.store, Hub: s.hub}
		api.POST("/equipment", equipmentHandlers.Create)
		api.POST("/equipment/bulk", equipmentHandlers.BulkCreate)
		api.GET("/equipment", equipmentHandlers.List)
		api.GET("/equipment/:id", equipmentHandlers.Get)
		api.PUT("/equipment/:id", equipmentHandlers.Update)
		api.DELETE("/equipment/:id", equipmentHandlers.Delete)
		api.POST("/equipment/notify", equipmentHandlers.Notify)

		sensorHandlers := &handlers.SensorHandlers{Store: s.store, Hub: s.hub}
		api.POST("/equipment/:id/sensors", sensorHandlers.AddSensor)
		api.GET("/equipment/:id/sensors", sensorHandlers.ListSensors)
		api.GET("/sensors/:id", sensorHandlers.GetSensor)
		api.DELETE("/sensors/:id", sensorHandlers.DeleteSensor)
		api.PUT("/sensors/:id/value", sensorHandlers.SetValue)

		actuatorHandlers := &handlers.ActuatorHandlers{Store: s.store, Hub: s.hub}
		api.POST("/equipment/:id/actuators", actuatorHandlers.AddActuator)
		api.GET("/equipment/:id/actuators", actuatorHandlers.ListActuators)
		api.GET("/actuators/:id", actuatorHandlers.GetActuator)
		api.DELETE("/actuators/:id", actuatorHandlers.DeleteActuator)
		api.PUT("/actuators/:id/state", actuatorHandlers.SetState)

		occupancyHandlers := &handlers.OccupancyHandlers{Store: s.store, Hub: s.hub}
		api.GET("/occupancy", occupancyHandlers.Get)
		api.PUT("/occupancy", occupancyHandlers.Set)

		coverageHandlers := &handlers.CoverageHandlers{Store: s.store, Hub: s.hub}
		api.GET("/coverage", coverageHandlers.Get)
		api.PUT("/coverage", coverageHandlers.Set)

		visualizationHandlers := &handlers.VisualizationHandlers{Store: s.store, Hub: s.hub}
		api.GET("/room-layers", visualizationHandlers.GetRoomLayers)
		api.PUT("/room-layers", visualizationHandlers.SetRoomLayers)
		api.GET("/effects", visualizationHandlers.GetEffects)
		api.PUT("/effects", visualizationHandlers.SetEffects)
		api.GET("/entities", visualizationHandlers.GetEntities)
		api.PUT("/entities", visualizationHandlers.SetEntities)
		api.GET("/room-appearance", visualizationHandlers.GetRoomAppearance)
		api.PUT("/room-appearance", visualizationHandlers.SetRoomAppearance)
		api.GET("/alerts", visualizationHandlers.GetAlerts)
		api.PUT("/alerts", visualizationHandlers.SetAlerts)
		api.GET("/doors", visualizationHandlers.GetDoors)
		api.PUT("/doors", visualizationHandlers.SetDoors)

		sessionHandlers := &handlers.SessionHandlers{Store: s.store, Hub: s.hub}
		api.GET("/sessions", sessionHandlers.List)
		api.POST("/sessions", sessionHandlers.Create)
		api.GET("/sessions/:id", sessionHandlers.Get)
		api.DELETE("/sessions/:id", sessionHandlers.Delete)
		api.PUT("/sessions/:id/viewport", sessionHandlers.SetViewport)
		api.PUT("/sessions/:id/highlights", sessionHandlers.SetHighlights)
		api.PUT("/sessions/:id/occupancy", sessionHandlers.SetOccupancy)
		api.PUT("/sessions/:id/route", sessionHandlers.SetRoute)
		api.PUT("/sessions/:id/coverage", sessionHandlers.SetCoverage)

		api.GET("/icons/:name", func(c *gin.Context) {
			name := c.Param("name")
			if path.Base(name) != name || !strings.HasSuffix(strings.ToLower(name), ".svg") {
				c.JSON(http.StatusBadRequest, gin.H{"error": "invalid icon name"})
				return
			}
			data, err := fs.ReadFile(s.dataFS, "data/equipment/icons/"+name)
			if err != nil {
				c.JSON(http.StatusNotFound, gin.H{"error": "icon not found"})
				return
			}
			c.Header("Cache-Control", "public, max-age=3600")
			c.Data(http.StatusOK, "image/svg+xml", data)
		})
	}

	sessionHandlers := &handlers.SessionHandlers{Store: s.store, Hub: s.hub}
	router.GET("/ws/:id", sessionHandlers.HandleWebSocket)

	if s.EditMode {
		editor := handlers.NewEditorHandlers(s.store, s.EditOutputDir)
		router.POST("/api/editor/floors/:level", editor.SaveFloor)
		router.POST("/api/editor/notes/:building", editor.SaveNotes)
	}

	vendor, err := fs.Sub(s.webFS, "web/vendor")
	if err != nil {
		return nil, fmt.Errorf("open shared web assets: %w", err)
	}
	router.StaticFS("/vendor", http.FS(vendor))

	uiRoot := "web"
	if s.UI == UIModern {
		uiRoot = "web/modern"
	}
	selectedUI, err := fs.Sub(s.webFS, uiRoot)
	if err != nil {
		return nil, fmt.Errorf("open %s UI: %w", s.UI, err)
	}
	router.NoRoute(gin.WrapH(http.FileServer(http.FS(selectedUI))))
	return router, nil
}

func (s *Server) Start() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return s.StartContext(ctx)
}

func (s *Server) StartContext(ctx context.Context) error {
	closeLog, err := s.configureLogging()
	if err != nil {
		return err
	}
	defer closeLog()

	router, err := s.Router()
	if err != nil {
		return err
	}
	purgeCtx, cancelPurge := context.WithCancel(ctx)
	defer cancelPurge()
	s.hub.StartPurger(purgeCtx, 5*time.Minute, time.Hour)

	server := &http.Server{
		Addr:              s.Address(),
		Handler:           router,
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}
	errCh := make(chan error, 1)
	go func() { errCh <- server.ListenAndServe() }()
	log.Printf("BuildSim server starting on http://%s (ui=%s)", s.Address(), s.UI)

	select {
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("shutdown server: %w", err)
		}
		return nil
	}
}

func (s *Server) configureLogging() (func(), error) {
	previousLogWriter := log.Writer()
	previousGinWriter := gin.DefaultWriter
	previousGinErrorWriter := gin.DefaultErrorWriter
	writer := io.Writer(os.Stdout)
	var file *os.File
	if strings.TrimSpace(s.LogFile) != "" {
		var err error
		file, err = os.OpenFile(s.LogFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			return nil, fmt.Errorf("open log file: %w", err)
		}
		writer = io.MultiWriter(os.Stdout, file)
	}
	log.SetOutput(writer)
	gin.DefaultWriter = writer
	gin.DefaultErrorWriter = writer
	return func() {
		log.SetOutput(previousLogWriter)
		gin.DefaultWriter = previousGinWriter
		gin.DefaultErrorWriter = previousGinErrorWriter
		if file != nil {
			_ = file.Close()
		}
	}, nil
}

func localCORSMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		origin := c.Request.Header.Get("Origin")
		if origin != "" {
			if !websocket.IsAllowedOrigin(c.Request) {
				c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "origin is not allowed"})
				return
			}
			c.Header("Access-Control-Allow-Origin", origin)
			c.Header("Vary", "Origin")
			c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			c.Header("Access-Control-Allow-Headers", "Content-Type, Accept")
			c.Header("Access-Control-Max-Age", "600")
		}
		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}

func defaultHost(host string) string {
	if strings.TrimSpace(host) == "" {
		return "127.0.0.1"
	}
	return host
}
