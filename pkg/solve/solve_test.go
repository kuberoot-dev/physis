package solve

import (
	"strings"
	"testing"

	"github.com/kuberoot-dev/physis/pkg/problem"
)

// A 4-cycle with unit weights: the best cut takes all 4 edges.
func cycle4(t *testing.T) *problem.Graph {
	t.Helper()
	g, err := problem.ReadGset("c4", strings.NewReader("4 4\n1 2 1\n2 3 1\n3 4 1\n4 1 1\n"))
	if err != nil {
		t.Fatal(err)
	}
	return g
}

func TestExactMaxCutOnCycle(t *testing.T) {
	r := ExactMaxCut(cycle4(t))
	if r.Cut != 4 {
		t.Fatalf("cut = %v, want 4", r.Cut)
	}
	if got := cycle4(t).Cut(r.Spins); got != r.Cut {
		t.Fatalf("reported cut %v disagrees with spins %v", r.Cut, got)
	}
}

// Every heuristic must report a cut that its own spins reproduce, and never
// exceed the exact optimum.
func TestHeuristicsAgreeWithTheirSpinsAndNeverBeatExact(t *testing.T) {
	for seed := uint64(1); seed <= 5; seed++ {
		g := problem.RandomGraph(18, 0.3, seed)
		exact := ExactMaxCut(g).Cut
		for name, r := range map[string]CutResult{
			"local":  LocalSearch(g, 20, seed),
			"anneal": Anneal(g, 200, 2, 0.05, seed),
			"pbit":   PBitAnneal(g, 200, 0.1, 3, seed),
		} {
			if got := g.Cut(r.Spins); got != r.Cut {
				t.Fatalf("seed %d %s: reported %v, spins give %v", seed, name, r.Cut, got)
			}
			if r.Cut > exact {
				t.Fatalf("seed %d %s: %v beats exact %v", seed, name, r.Cut, exact)
			}
			if r.Ops.SpinUpdates == 0 {
				t.Fatalf("%s counted no operations", name)
			}
		}
	}
}

// Annealing long enough on small instances reaches the optimum: the quality
// curve the scheduler relies on is real, not assumed.
func TestAnnealReachesOptimumOnSmallGraphs(t *testing.T) {
	hits := 0
	for seed := uint64(1); seed <= 10; seed++ {
		g := problem.RandomGraph(16, 0.4, seed)
		if Anneal(g, 2000, 2, 0.02, seed).Cut == ExactMaxCut(g).Cut {
			hits++
		}
	}
	if hits < 9 {
		t.Fatalf("annealing found the optimum in %d/10 instances", hits)
	}
}

func TestOpsScaleWithSweeps(t *testing.T) {
	g := problem.RandomGraph(30, 0.2, 7)
	a := Anneal(g, 100, 2, 0.05, 1).Ops
	b := Anneal(g, 200, 2, 0.05, 1).Ops
	if b.SpinUpdates != 2*a.SpinUpdates || b.MACs != 2*a.MACs {
		t.Fatalf("ops not linear in sweeps: %+v vs %+v", a, b)
	}
}
