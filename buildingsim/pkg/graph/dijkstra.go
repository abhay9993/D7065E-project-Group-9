package graph

import (
	"container/heap"
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/eislab-cps/buildingsim/pkg/model"
)

var (
	ErrNodeNameNotFound  = errors.New("navigation node name not found")
	ErrAmbiguousNodeName = errors.New("navigation node name is present on more than one floor")
)

type adjacency struct {
	to     int
	weight float64
}

type item struct {
	node int
	dist float64
	idx  int
}

type priorityQueue []*item

func (pq priorityQueue) Len() int           { return len(pq) }
func (pq priorityQueue) Less(i, j int) bool { return pq[i].dist < pq[j].dist }
func (pq priorityQueue) Swap(i, j int)      { pq[i], pq[j] = pq[j], pq[i]; pq[i].idx = i; pq[j].idx = j }
func (pq *priorityQueue) Push(x interface{}) {
	it := x.(*item)
	it.idx = len(*pq)
	*pq = append(*pq, it)
}
func (pq *priorityQueue) Pop() interface{} {
	old := *pq
	n := len(old)
	it := old[n-1]
	*pq = old[:n-1]
	return it
}

// ShortestPath computes Dijkstra's shortest path between two room IDs on a single floor.
func ShortestPath(graph *model.NavGraph, fromID, toID int) *model.RouteResult {
	if graph == nil {
		return nil
	}

	nodeIndex := make(map[int]int) // room id -> index in nodes
	for i, n := range graph.Nodes {
		nodeIndex[n.ID] = i
	}

	if _, ok := nodeIndex[fromID]; !ok {
		return nil
	}
	if _, ok := nodeIndex[toID]; !ok {
		return nil
	}

	// Build adjacency list
	n := len(graph.Nodes)
	adj := make([][]adjacency, n)
	for i := range adj {
		adj[i] = []adjacency{}
	}
	for _, e := range graph.Edges {
		fi, ok1 := nodeIndex[e.From]
		ti, ok2 := nodeIndex[e.To]
		if !ok1 || !ok2 {
			continue
		}
		adj[fi] = append(adj[fi], adjacency{to: ti, weight: e.Weight})
		adj[ti] = append(adj[ti], adjacency{to: fi, weight: e.Weight})
	}

	// Dijkstra
	dist := make([]float64, n)
	prev := make([]int, n)
	for i := range dist {
		dist[i] = math.Inf(1)
		prev[i] = -1
	}

	startIdx := nodeIndex[fromID]
	endIdx := nodeIndex[toID]
	dist[startIdx] = 0

	pq := &priorityQueue{&item{node: startIdx, dist: 0}}
	heap.Init(pq)

	for pq.Len() > 0 {
		cur := heap.Pop(pq).(*item)
		if cur.dist > dist[cur.node] {
			continue
		}
		if cur.node == endIdx {
			break
		}
		for _, edge := range adj[cur.node] {
			newDist := dist[cur.node] + edge.weight
			if newDist < dist[edge.to] {
				dist[edge.to] = newDist
				prev[edge.to] = cur.node
				heap.Push(pq, &item{node: edge.to, dist: newDist})
			}
		}
	}

	if math.IsInf(dist[endIdx], 1) {
		return nil
	}

	// Reconstruct path
	var path []model.RouteNode
	for idx := endIdx; idx != -1; idx = prev[idx] {
		node := graph.Nodes[idx]
		path = append([]model.RouteNode{{
			RoomID: node.ID,
			Name:   node.Name,
			Level:  node.Level,
			X:      node.X,
			Y:      node.Y,
		}}, path...)
	}

	return &model.RouteResult{
		Path:     path,
		Distance: dist[endIdx],
	}
}

// ShortestPathByName finds nodes by name (room label) and computes shortest path.
// For walkable graphs, multiple nodes may share the same name (room node + entry node).
// We pick the "room" type node if available.
func ShortestPathByName(graph *model.NavGraph, fromName, toName string) *model.RouteResult {
	result, err := ShortestPathByQualifiedName(graph, fromName, "", toName, "")
	if err != nil {
		return nil
	}
	return result
}

// ShortestPathByQualifiedName resolves room labels together with optional
// floor qualifiers. A label that occurs on several floors is rejected when no
// qualifier is supplied; silently choosing a floor can produce a valid-looking
// but physically incorrect route. Duplicate room/entry nodes on one floor are
// expected in walkable graphs, and room nodes are preferred.
func ShortestPathByQualifiedName(graph *model.NavGraph, fromName, fromLevel, toName, toLevel string) (*model.RouteResult, error) {
	if graph == nil {
		return nil, ErrNodeNameNotFound
	}

	fromID, err := resolveNamedNode(graph, fromName, fromLevel)
	if err != nil {
		return nil, fmt.Errorf("resolve from_name %q: %w", fromName, err)
	}
	toID, err := resolveNamedNode(graph, toName, toLevel)
	if err != nil {
		return nil, fmt.Errorf("resolve to_name %q: %w", toName, err)
	}
	return ShortestPath(graph, fromID, toID), nil
}

// WithoutNodes returns an independent graph snapshot without blocked nodes or
// their incident edges. It is useful for dynamic constraints such as locked
// doors while leaving the immutable floor-plan graph untouched.
func WithoutNodes(graph *model.NavGraph, blocked map[int]bool) *model.NavGraph {
	if graph == nil {
		return nil
	}
	if len(blocked) == 0 {
		copy := *graph
		copy.Nodes = append([]model.NavNode(nil), graph.Nodes...)
		copy.Edges = append([]model.NavEdge(nil), graph.Edges...)
		return &copy
	}
	filtered := &model.NavGraph{
		Nodes: make([]model.NavNode, 0, len(graph.Nodes)),
		Edges: make([]model.NavEdge, 0, len(graph.Edges)),
	}
	for _, node := range graph.Nodes {
		if !blocked[node.ID] {
			filtered.Nodes = append(filtered.Nodes, node)
		}
	}
	for _, edge := range graph.Edges {
		if !blocked[edge.From] && !blocked[edge.To] {
			filtered.Edges = append(filtered.Edges, edge)
		}
	}
	return filtered
}

func resolveNamedNode(graph *model.NavGraph, name, level string) (int, error) {
	name = strings.TrimSpace(name)
	level = extractLevel(strings.TrimSpace(level))
	bestID := -1
	bestIsRoom := false
	matchedLevels := make(map[string]struct{})

	for _, node := range graph.Nodes {
		if node.Name != name {
			continue
		}
		nodeLevel := extractLevel(strings.TrimSpace(node.Level))
		if level != "" && nodeLevel != level {
			continue
		}
		matchedLevels[nodeLevel] = struct{}{}
		isRoom := node.Type == "room"
		if bestID == -1 || (isRoom && !bestIsRoom) {
			bestID = node.ID
			bestIsRoom = isRoom
		}
	}

	if bestID == -1 {
		return -1, ErrNodeNameNotFound
	}
	if level == "" && len(matchedLevels) > 1 {
		return -1, ErrAmbiguousNodeName
	}
	return bestID, nil
}
