// Package problem holds the workloads physis schedules: weighted MaxCut
// (a combinatorial optimisation that maps directly onto Ising hardware) and
// linear classification (an inference workload made of matrix-vector
// products, the native operation of analog in-memory computing).
package problem

import (
	"bufio"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"math/rand/v2"
	"strconv"
	"strings"
)

// Edge is an undirected weighted edge between two vertices.
type Edge struct {
	U, V int
	W    float64
}

// Graph is an undirected weighted graph in adjacency form. The MaxCut of a
// graph is the partition of its vertices into two sets that maximises the
// total weight of the edges crossing between them. With spins s_i in {-1,+1}
// the cut equals sum_{(i,j)} w_ij (1 - s_i s_j) / 2, which is the Ising
// energy up to sign and offset: this is why MaxCut is the canonical benchmark
// of Ising machines.
type Graph struct {
	Name  string
	N     int
	Edges []Edge
	// Adjacency: for vertex i, neighbours Nbr[i][k] with weights Wt[i][k].
	Nbr [][]int
	Wt  [][]float64
	// BestKnown is the best published cut value, 0 when unknown.
	BestKnown float64
}

// NewGraph builds the adjacency structure of a graph from its edges.
func NewGraph(name string, n int, edges []Edge) (*Graph, error) {
	g := &Graph{Name: name, N: n, Edges: edges, Nbr: make([][]int, n), Wt: make([][]float64, n)}
	for _, e := range edges {
		if e.U < 0 || e.V < 0 || e.U >= n || e.V >= n {
			return nil, fmt.Errorf("edge %d-%d outside 0..%d", e.U, e.V, n-1)
		}
		if e.U == e.V {
			return nil, fmt.Errorf("self loop at %d", e.U)
		}
		g.Nbr[e.U] = append(g.Nbr[e.U], e.V)
		g.Wt[e.U] = append(g.Wt[e.U], e.W)
		g.Nbr[e.V] = append(g.Nbr[e.V], e.U)
		g.Wt[e.V] = append(g.Wt[e.V], e.W)
	}
	return g, nil
}

// Degree sum, the number of multiply-accumulate operations a full sweep of
// local-field updates costs on digital hardware.
func (g *Graph) DegreeSum() int { return 2 * len(g.Edges) }

// Cut returns the weight of the cut given by the spin assignment s.
func (g *Graph) Cut(s []int8) float64 {
	var c float64
	for _, e := range g.Edges {
		if s[e.U] != s[e.V] {
			c += e.W
		}
	}
	return c
}

// TotalWeight is the sum of all edge weights, an upper bound on the cut.
func (g *Graph) TotalWeight() float64 {
	var t float64
	for _, e := range g.Edges {
		t += e.W
	}
	return t
}

// Digest is a content hash of the graph, used in receipts so that anyone can
// check which instance a result belongs to.
func (g *Graph) Digest() string {
	h := sha256.New()
	var buf [8]byte
	binary.LittleEndian.PutUint64(buf[:], uint64(g.N))
	h.Write(buf[:])
	for _, e := range g.Edges {
		binary.LittleEndian.PutUint64(buf[:], uint64(e.U))
		h.Write(buf[:])
		binary.LittleEndian.PutUint64(buf[:], uint64(e.V))
		h.Write(buf[:])
		h.Write([]byte(strconv.FormatFloat(e.W, 'g', -1, 64)))
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}

// ReadGset parses the format of the Gset MaxCut benchmark (Helmberg and
// Rendl): a header "n m", then m lines "i j w" with 1-based vertices.
func ReadGset(name string, r io.Reader) (*Graph, error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	if !sc.Scan() {
		return nil, fmt.Errorf("%s: empty input", name)
	}
	var n, m int
	if _, err := fmt.Sscan(sc.Text(), &n, &m); err != nil {
		return nil, fmt.Errorf("%s: header: %w", name, err)
	}
	edges := make([]Edge, 0, m)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var i, j int
		var w float64
		if _, err := fmt.Sscan(line, &i, &j, &w); err != nil {
			return nil, fmt.Errorf("%s: edge %q: %w", name, line, err)
		}
		edges = append(edges, Edge{U: i - 1, V: j - 1, W: w})
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if len(edges) != m {
		return nil, fmt.Errorf("%s: header says %d edges, read %d", name, m, len(edges))
	}
	return NewGraph(name, n, edges)
}

// GsetBestKnown are best published cut values that we confirmed in two
// independent sources (ref:gset-best). For other instances physis uses the
// best cut found across all its own runs as the reference and says so.
var GsetBestKnown = map[string]float64{
	"G1": 11624, "G2": 11620, "G22": 13359,
}

// RandomGraph draws an Erdos-Renyi G(n, p) graph with unit weights, seeded so
// that every run of an experiment sees the same instance.
func RandomGraph(n int, p float64, seed uint64) *Graph {
	rng := rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15))
	var edges []Edge
	for i := 0; i < n; i++ {
		for j := i + 1; j < n; j++ {
			if rng.Float64() < p {
				edges = append(edges, Edge{U: i, V: j, W: 1})
			}
		}
	}
	g, _ := NewGraph(fmt.Sprintf("er-n%d-p%.2f-s%d", n, p, seed), n, edges)
	return g
}
