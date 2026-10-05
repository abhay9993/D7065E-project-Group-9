package handlers

import (
	"errors"
	"math"
	"net/http"
	"strconv"
	"strings"

	"github.com/eislab-cps/buildingsim/pkg/graph"
	"github.com/eislab-cps/buildingsim/pkg/model"
	"github.com/eislab-cps/buildingsim/pkg/store"
	"github.com/gin-gonic/gin"
)

type GraphHandlers struct {
	Store *store.MemoryStore
}

func (h *GraphHandlers) getNavGraph(c *gin.Context, data *model.FloorData) *model.NavGraph {
	graphType := c.DefaultQuery("type", "adjacency")
	if graphType == "walkable" {
		return data.WalkableGraph
	}
	return data.Graph
}

func (h *GraphHandlers) GetGraph(c *gin.Context) {
	level := c.DefaultQuery("level", "level0")
	data, ok := h.Store.GetFloorData(level)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "level not found"})
		return
	}
	g := h.getNavGraph(c, data)
	if g == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "graph not found"})
		return
	}
	c.JSON(http.StatusOK, g)
}

func (h *GraphHandlers) GetRoute(c *gin.Context) {
	level := c.Query("level")
	graphType := c.DefaultQuery("type", "adjacency")

	var result *model.RouteResult

	fromName := c.Query("from_name")
	toName := c.Query("to_name")
	fromLevel := c.Query("from_level")
	toLevel := c.Query("to_level")

	if fromName != "" && toName != "" && graphType == "walkable" && level == "" {
		// Multi-floor walkable routing
		g := h.Store.GetMultiFloorGraph()
		if g == nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "multi-floor graph not available"})
			return
		}
		g = graph.WithoutNodes(g, blockedDoorNodes(g, h.Store.GetDoors(), ""))
		var err error
		result, err = graph.ShortestPathByQualifiedName(g, fromName, fromLevel, toName, toLevel)
		if err != nil {
			if errors.Is(err, graph.ErrAmbiguousNodeName) {
				c.JSON(http.StatusBadRequest, gin.H{
					"error": err.Error() + "; provide from_level and to_level",
				})
				return
			}
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
	} else {
		// Single-floor routing
		if level == "" {
			level = "level0"
		}
		data, ok := h.Store.GetFloorData(level)
		if !ok {
			c.JSON(http.StatusNotFound, gin.H{"error": "level not found"})
			return
		}
		g := h.getNavGraph(c, data)
		if g == nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "graph not found"})
			return
		}
		if graphType == "walkable" {
			g = graph.WithoutNodes(g, blockedDoorNodes(g, h.Store.GetDoors(), level))
		}

		if fromName != "" && toName != "" {
			result = graph.ShortestPathByName(g, fromName, toName)
		} else {
			fromStr := c.Query("from")
			toStr := c.Query("to")
			from, err := strconv.Atoi(fromStr)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "invalid 'from' room id"})
				return
			}
			to, err := strconv.Atoi(toStr)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "invalid 'to' room id"})
				return
			}
			result = graph.ShortestPath(g, from, to)
		}
	}

	if result == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "no route found"})
		return
	}

	c.JSON(http.StatusOK, result)
}

func blockedDoorNodes(nav *model.NavGraph, doors []model.Door, defaultLevel string) map[int]bool {
	blocked := make(map[int]bool)
	for _, node := range nav.Nodes {
		if node.Type != "entry" {
			continue
		}
		nodeLevel := strings.TrimSpace(node.Level)
		if nodeLevel == "" {
			nodeLevel = defaultLevel
		}
		for _, door := range doors {
			if !door.Blocked || door.Level != nodeLevel {
				continue
			}
			if node.Level == "" && door.EntryNodeID != nil && node.ID == *door.EntryNodeID {
				blocked[node.ID] = true
				break
			}
			if door.Room == "" || node.Name != door.Room {
				continue
			}
			// In a merged graph IDs are remapped. A published doorway position
			// identifies one entry precisely; room-only locks intentionally close
			// every entry for that room.
			if door.EntryNodeID == nil || door.Position == nil ||
				(math.Abs(node.X-door.Position[0]) < 1e-6 && math.Abs(node.Y-door.Position[1]) < 1e-6) {
				blocked[node.ID] = true
				break
			}
		}
	}
	return blocked
}
