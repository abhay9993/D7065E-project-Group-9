package sim

import (
	"container/heap"
	"math"
)

// Node and Edge mirror the BuildSim walkable graph.
type Node struct {
	ID   int
	Name string
	X, Y float64
	Type string // corridor, entry, room
}

type Edge struct {
	From, To int
	Weight   float64
}

type neighbour struct {
	to     int
	weight float64
}

// Graph is an in-memory copy of one floor's walkable graph with a route cache.
// Routes are computed locally rather than through GET /api/graph/route so that
// hundreds of agents can replan every simulated day without one HTTP round trip
// each, and so that the simulation keeps running while BuildSim restarts.
type Graph struct {
	Nodes    []Node
	adj      [][]neighbour
	byID     map[int]int
	roomNode map[string]int
	cache    map[[2]int][]int
}

func NewGraph(nodes []Node, edges []Edge) *Graph {
	g := &Graph{
		Nodes:    append([]Node(nil), nodes...),
		adj:      make([][]neighbour, len(nodes)),
		byID:     make(map[int]int, len(nodes)),
		roomNode: map[string]int{},
		cache:    map[[2]int][]int{},
	}
	for i, node := range g.Nodes {
		g.byID[node.ID] = i
		if node.Type == "room" {
			if _, exists := g.roomNode[node.Name]; !exists {
				g.roomNode[node.Name] = i
			}
		}
	}
	for _, edge := range edges {
		from, ok1 := g.byID[edge.From]
		to, ok2 := g.byID[edge.To]
		if !ok1 || !ok2 {
			continue
		}
		g.adj[from] = append(g.adj[from], neighbour{to, edge.Weight})
		g.adj[to] = append(g.adj[to], neighbour{from, edge.Weight})
	}
	return g
}

// RoomNode returns the index of a room's centre node, or -1.
func (g *Graph) RoomNode(name string) int {
	if idx, ok := g.roomNode[name]; ok {
		return idx
	}
	return -1
}

// NearestNamed returns the node with the given name closest to a point, or -1.
// Corridors have several centreline nodes, so the one nearest the corridor's
// centre represents it.
func (g *Graph) NearestNamed(name string, point [2]float64) int {
	best, bestDist := -1, math.Inf(1)
	for i, node := range g.Nodes {
		if node.Name != name {
			continue
		}
		d := math.Hypot(node.X-point[0], node.Y-point[1])
		if d < bestDist {
			best, bestDist = i, d
		}
	}
	return best
}

// Nearest returns the index of the node closest to a point.
func (g *Graph) Nearest(point [2]float64) int {
	best, bestDist := -1, math.Inf(1)
	for i, node := range g.Nodes {
		d := math.Hypot(node.X-point[0], node.Y-point[1])
		if d < bestDist {
			best, bestDist = i, d
		}
	}
	return best
}

// Route returns node indices from one node to another, inclusive, or nil when
// no path exists. Results are cached; the graph is immutable.
func (g *Graph) Route(from, to int) []int {
	if from < 0 || to < 0 || from >= len(g.Nodes) || to >= len(g.Nodes) {
		return nil
	}
	if from == to {
		return []int{from}
	}
	key := [2]int{from, to}
	if route, ok := g.cache[key]; ok {
		return route
	}
	dist := make([]float64, len(g.Nodes))
	prev := make([]int, len(g.Nodes))
	for i := range dist {
		dist[i] = math.Inf(1)
		prev[i] = -1
	}
	dist[from] = 0
	pq := &queue{{node: from}}
	for pq.Len() > 0 {
		cur := heap.Pop(pq).(item)
		if cur.dist > dist[cur.node] {
			continue
		}
		if cur.node == to {
			break
		}
		for _, n := range g.adj[cur.node] {
			next := cur.dist + n.weight
			if next < dist[n.to] {
				dist[n.to] = next
				prev[n.to] = cur.node
				heap.Push(pq, item{node: n.to, dist: next})
			}
		}
	}
	if math.IsInf(dist[to], 1) {
		g.cache[key] = nil
		return nil
	}
	var route []int
	for at := to; at != -1; at = prev[at] {
		route = append([]int{at}, route...)
	}
	g.cache[key] = route
	return route
}

// Length returns the length of a route in floor-plan units.
func (g *Graph) Length(route []int) float64 {
	total := 0.0
	for i := 1; i < len(route); i++ {
		a, b := g.Nodes[route[i-1]], g.Nodes[route[i]]
		total += math.Hypot(a.X-b.X, a.Y-b.Y)
	}
	return total
}

type item struct {
	node int
	dist float64
}

type queue []item

func (q queue) Len() int            { return len(q) }
func (q queue) Less(i, j int) bool  { return q[i].dist < q[j].dist }
func (q queue) Swap(i, j int)       { q[i], q[j] = q[j], q[i] }
func (q *queue) Push(x interface{}) { *q = append(*q, x.(item)) }
func (q *queue) Pop() interface{} {
	old := *q
	it := old[len(old)-1]
	*q = old[:len(old)-1]
	return it
}
