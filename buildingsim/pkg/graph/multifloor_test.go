package graph

import (
	"testing"

	"github.com/eislab-cps/buildingsim/pkg/model"
)

func TestBuildMultiFloorGraphRemapsIDsAndAddsConnector(t *testing.T) {
	floors := map[string]*model.FloorData{
		"level0": {WalkableGraph: floorGraph("A", "Stair", 1)},
		"level1": {WalkableGraph: floorGraph("B", "Stair", 1)},
	}
	edges := []model.CrossFloorEdge{{
		FromLevel: "building/level0", FromName: "Stair",
		ToLevel: "building/level1", ToName: "Stair", Type: "stair",
	}}
	merged := BuildMultiFloorGraph(floors, []string{"level0", "level1"}, edges)
	if len(merged.Nodes) != 4 || len(merged.Edges) != 3 {
		t.Fatalf("unexpected merged graph size: %d nodes, %d edges", len(merged.Nodes), len(merged.Edges))
	}
	for i, node := range merged.Nodes {
		if node.ID != i || node.Level == "" {
			t.Fatalf("node was not remapped/annotated: %+v", node)
		}
	}
	connector := merged.Edges[len(merged.Edges)-1]
	if connector.Weight != 20 {
		t.Fatalf("stair connector weight = %v", connector.Weight)
	}
	result := ShortestPathByName(merged, "A", "B")
	if result == nil || result.Distance != 22 {
		t.Fatalf("unexpected cross-floor route: %+v", result)
	}
}

func TestBuildMultiFloorGraphUsesElevatorWeight(t *testing.T) {
	floors := map[string]*model.FloorData{
		"level0": {WalkableGraph: floorGraph("A", "Lift", 2)},
		"level1": {WalkableGraph: floorGraph("B", "Lift", 3)},
	}
	merged := BuildMultiFloorGraph(floors, []string{"level0", "missing", "level1"}, []model.CrossFloorEdge{{
		FromLevel: "level0", FromName: "Lift", ToLevel: "level1", ToName: "Lift", Type: "elevator",
	}})
	result := ShortestPathByName(merged, "A", "B")
	if result == nil || result.Distance != 15 {
		t.Fatalf("unexpected elevator route: %+v", result)
	}
}

func TestBuildMultiFloorGraphSkipsInvalidConnectors(t *testing.T) {
	merged := BuildMultiFloorGraph(map[string]*model.FloorData{
		"level0": {WalkableGraph: floorGraph("A", "Stair", 1)},
	}, []string{"level0"}, []model.CrossFloorEdge{{
		FromLevel: "level0", FromName: "missing", ToLevel: "level9", ToName: "missing",
	}})
	if len(merged.Nodes) != 2 || len(merged.Edges) != 1 {
		t.Fatalf("invalid connector changed graph: %+v", merged)
	}
}

func floorGraph(room, connector string, weight float64) *model.NavGraph {
	return &model.NavGraph{
		Nodes: []model.NavNode{
			{ID: 0, Name: room, Type: "room"},
			{ID: 1, Name: connector, Type: "entry"},
		},
		Edges: []model.NavEdge{{From: 0, To: 1, Weight: weight}},
	}
}
