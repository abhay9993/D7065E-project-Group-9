package graph

import (
	"errors"
	"math"
	"testing"

	"github.com/eislab-cps/buildingsim/pkg/model"
)

func TestShortestPathUsesLowestWeight(t *testing.T) {
	graph := &model.NavGraph{
		Nodes: []model.NavNode{{ID: 10, Name: "A"}, {ID: 20, Name: "B"}, {ID: 30, Name: "C"}},
		Edges: []model.NavEdge{
			{From: 10, To: 30, Weight: 10},
			{From: 10, To: 20, Weight: 2},
			{From: 20, To: 30, Weight: 3},
		},
	}
	result := ShortestPath(graph, 10, 30)
	if result == nil || result.Distance != 5 || len(result.Path) != 3 || result.Path[1].RoomID != 20 {
		t.Fatalf("unexpected route: %+v", result)
	}
}

func TestQualifiedNameRejectsCrossFloorAmbiguity(t *testing.T) {
	graph := &model.NavGraph{
		Nodes: []model.NavNode{
			{ID: 1, Name: "Shared", Type: "room", Level: "level0"},
			{ID: 2, Name: "Target", Type: "room", Level: "level0"},
			{ID: 3, Name: "Shared", Type: "entry", Level: "level1"},
			{ID: 4, Name: "Shared", Type: "room", Level: "level1"},
			{ID: 5, Name: "Target", Type: "room", Level: "level1"},
		},
		Edges: []model.NavEdge{
			{From: 1, To: 2, Weight: 2},
			{From: 3, To: 4, Weight: 1},
			{From: 4, To: 5, Weight: 3},
		},
	}
	if _, err := ShortestPathByQualifiedName(graph, "Shared", "", "Target", "level0"); !errors.Is(err, ErrAmbiguousNodeName) {
		t.Fatalf("error=%v, want ambiguity", err)
	}
	result, err := ShortestPathByQualifiedName(graph, "Shared", "building/level1", "Target", "level1")
	if err != nil || result == nil || result.Path[0].RoomID != 4 || result.Distance != 3 {
		t.Fatalf("qualified route=%+v err=%v", result, err)
	}
}

func TestQualifiedNameReportsMissingEndpoint(t *testing.T) {
	graph := &model.NavGraph{Nodes: []model.NavNode{{ID: 1, Name: "A", Type: "room", Level: "level0"}}}
	if _, err := ShortestPathByQualifiedName(graph, "A", "level0", "missing", "level0"); !errors.Is(err, ErrNodeNameNotFound) {
		t.Fatalf("error=%v, want missing name", err)
	}
}

func TestShortestPathSameNode(t *testing.T) {
	graph := &model.NavGraph{Nodes: []model.NavNode{{ID: 1, Name: "A", X: 4, Y: 5}}}
	result := ShortestPath(graph, 1, 1)
	if result == nil || result.Distance != 0 || len(result.Path) != 1 || result.Path[0].Name != "A" {
		t.Fatalf("unexpected route: %+v", result)
	}
}

func TestShortestPathRejectsMissingAndDisconnectedNodes(t *testing.T) {
	graph := &model.NavGraph{Nodes: []model.NavNode{{ID: 1}, {ID: 2}}}
	for _, test := range []struct{ from, to int }{{0, 2}, {1, 0}, {1, 2}} {
		if result := ShortestPath(graph, test.from, test.to); result != nil {
			t.Fatalf("expected no route for %d -> %d, got %+v", test.from, test.to, result)
		}
	}
	if ShortestPath(nil, 1, 2) != nil {
		t.Fatal("nil graph should have no route")
	}
}

func TestShortestPathIgnoresEdgesForUnknownNodes(t *testing.T) {
	graph := &model.NavGraph{
		Nodes: []model.NavNode{{ID: 1}, {ID: 2}},
		Edges: []model.NavEdge{{From: 1, To: 99, Weight: 0}, {From: 1, To: 2, Weight: 7}},
	}
	result := ShortestPath(graph, 1, 2)
	if result == nil || math.Abs(result.Distance-7) > 1e-9 {
		t.Fatalf("unexpected route: %+v", result)
	}
}

func TestShortestPathByNamePrefersRoomNode(t *testing.T) {
	graph := &model.NavGraph{
		Nodes: []model.NavNode{
			{ID: 1, Name: "A", Type: "entry"},
			{ID: 2, Name: "A", Type: "room"},
			{ID: 3, Name: "B", Type: "room"},
		},
		Edges: []model.NavEdge{{From: 1, To: 3, Weight: 1}, {From: 2, To: 3, Weight: 5}},
	}
	result := ShortestPathByName(graph, "A", "B")
	if result == nil || result.Path[0].RoomID != 2 || result.Distance != 5 {
		t.Fatalf("unexpected route: %+v", result)
	}
	if ShortestPathByName(graph, "missing", "B") != nil {
		t.Fatal("missing name should not resolve")
	}
}

func TestWithoutNodesRemovesIncidentEdgesAndDoesNotMutateInput(t *testing.T) {
	original := &model.NavGraph{
		Nodes: []model.NavNode{{ID: 1}, {ID: 2}, {ID: 3}},
		Edges: []model.NavEdge{{From: 1, To: 2, Weight: 1}, {From: 2, To: 3, Weight: 1}, {From: 1, To: 3, Weight: 4}},
	}
	filtered := WithoutNodes(original, map[int]bool{2: true})
	if len(filtered.Nodes) != 2 || len(filtered.Edges) != 1 || filtered.Edges[0].From != 1 || filtered.Edges[0].To != 3 {
		t.Fatalf("filtered graph = %+v", filtered)
	}
	filtered.Nodes[0].Name = "changed"
	if len(original.Nodes) != 3 || len(original.Edges) != 3 || original.Nodes[0].Name == "changed" {
		t.Fatalf("input graph was mutated: %+v", original)
	}
	copy := WithoutNodes(original, nil)
	copy.Edges[0].Weight = 99
	if original.Edges[0].Weight == 99 {
		t.Fatal("unfiltered copy shares edge storage")
	}
	if WithoutNodes(nil, nil) != nil {
		t.Fatal("nil graph should remain nil")
	}
}
