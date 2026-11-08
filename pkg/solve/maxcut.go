// Package solve implements the algorithms physis runs. Each solver returns
// the result and an exact count of the elementary operations it performed,
// which is what the device models turn into time and energy. The count is the
// bridge between "what was computed" and "what it cost on a given physics".
package solve

import (
	"math"
	"math/rand/v2"

	"github.com/kuberoot-dev/physis/pkg/problem"
)

// Ops counts the elementary operations of a run.
type Ops struct {
	// SpinUpdates is the number of single-spin update attempts (one local
	// field evaluation and one accept/reject decision each).
	SpinUpdates int64
	// MACs is the number of multiply-accumulate operations, digital or analog.
	MACs int64
	// RandomBits is the number of random bits drawn (p-bits consume one per
	// update; a digital annealer consumes a pseudo-random number).
	RandomBits int64
}

// Add accumulates another count.
func (o *Ops) Add(p Ops) {
	o.SpinUpdates += p.SpinUpdates
	o.MACs += p.MACs
	o.RandomBits += p.RandomBits
}

// CutResult is a MaxCut solution.
type CutResult struct {
	Spins []int8
	Cut   float64
	Ops   Ops
}

func rngFor(seed uint64) *rand.Rand {
	return rand.New(rand.NewPCG(seed, seed^0xda3e39cb94b95bdb))
}

func randomSpins(n int, rng *rand.Rand) []int8 {
	s := make([]int8, n)
	for i := range s {
		if rng.IntN(2) == 0 {
			s[i] = -1
		} else {
			s[i] = 1
		}
	}
	return s
}

// gain is the change of the cut when vertex i flips: the sum of weights to
// same-side neighbours minus the sum to other-side neighbours.
func gain(g *problem.Graph, s []int8, i int) float64 {
	var d float64
	for k, j := range g.Nbr[i] {
		if s[j] == s[i] {
			d += g.Wt[i][k]
		} else {
			d -= g.Wt[i][k]
		}
	}
	return d
}

// ExactMaxCut enumerates all 2^(n-1) cuts in Gray-code order, updating the
// cut incrementally. It is the ground truth for small instances (n <= 26).
func ExactMaxCut(g *problem.Graph) CutResult {
	n := g.N
	if n > 26 {
		panic("ExactMaxCut: instance too large for enumeration")
	}
	s := make([]int8, n)
	for i := range s {
		s[i] = -1
	}
	cut := 0.0
	best := 0.0
	bestS := append([]int8(nil), s...)
	var ops Ops
	// Vertex n-1 stays fixed: a cut and its mirror are the same cut.
	total := uint64(1) << uint(n-1)
	for k := uint64(1); k < total; k++ {
		i := trailingZeros(k)
		d := gain(g, s, i)
		ops.SpinUpdates++
		ops.MACs += int64(len(g.Nbr[i]))
		s[i] = -s[i]
		cut += d
		if cut > best {
			best = cut
			copy(bestS, s)
		}
	}
	return CutResult{Spins: bestS, Cut: best, Ops: ops}
}

func trailingZeros(x uint64) int {
	n := 0
	for x&1 == 0 {
		x >>= 1
		n++
	}
	return n
}

// LocalSearch is the general-purpose baseline: random restarts of steepest
// one-flip ascent. It is what an engineer writes first on a CPU.
func LocalSearch(g *problem.Graph, restarts int, seed uint64) CutResult {
	rng := rngFor(seed)
	var ops Ops
	best := CutResult{Cut: math.Inf(-1)}
	for r := 0; r < restarts; r++ {
		s := randomSpins(g.N, rng)
		improved := true
		for improved {
			improved = false
			for i := 0; i < g.N; i++ {
				d := gain(g, s, i)
				ops.SpinUpdates++
				ops.MACs += int64(len(g.Nbr[i]))
				if d > 0 {
					s[i] = -s[i]
					improved = true
				}
			}
		}
		c := g.Cut(s)
		if c > best.Cut {
			best = CutResult{Spins: s, Cut: c}
		}
	}
	best.Ops = ops
	return best
}

// Anneal is Metropolis simulated annealing with a geometric temperature
// schedule. On a CPU it is software; on a digital annealer or an Ising
// machine the same dynamics run in dedicated hardware, which is how physis
// emulates that physics: the algorithm is executed here, the operation count
// is charged at the device's rate and energy.
func Anneal(g *problem.Graph, sweeps int, t0, t1 float64, seed uint64) CutResult {
	rng := rngFor(seed)
	s := randomSpins(g.N, rng)
	cut := g.Cut(s)
	best := cut
	bestS := append([]int8(nil), s...)
	var ops Ops
	alpha := 1.0
	if sweeps > 1 {
		alpha = math.Pow(t1/t0, 1/float64(sweeps-1))
	}
	t := t0
	for sw := 0; sw < sweeps; sw++ {
		for i := 0; i < g.N; i++ {
			d := gain(g, s, i)
			ops.SpinUpdates++
			ops.MACs += int64(len(g.Nbr[i]))
			ops.RandomBits += 32
			if d >= 0 || rng.Float64() < math.Exp(d/t) {
				s[i] = -s[i]
				cut += d
				if cut > best {
					best = cut
					copy(bestS, s)
				}
			}
		}
		t *= alpha
	}
	return CutResult{Spins: bestS, Cut: best, Ops: ops}
}

// PBitAnneal runs Gibbs sampling of the Ising Boltzmann distribution with an
// increasing inverse temperature, the operating mode of probabilistic-bit
// (p-bit) computers: each p-bit takes the value +1 with probability
// sigma(2*beta*h_i) given its local field h_i. One random bit per update is
// what a stochastic magnetic tunnel junction supplies.
func PBitAnneal(g *problem.Graph, sweeps int, beta0, beta1 float64, seed uint64) CutResult {
	rng := rngFor(seed)
	s := randomSpins(g.N, rng)
	cut := g.Cut(s)
	best := cut
	bestS := append([]int8(nil), s...)
	var ops Ops
	for sw := 0; sw < sweeps; sw++ {
		beta := beta0
		if sweeps > 1 {
			beta = beta0 + (beta1-beta0)*float64(sw)/float64(sweeps-1)
		}
		for i := 0; i < g.N; i++ {
			// For MaxCut the coupling is antiferromagnetic: J_ij = -w_ij,
			// so the local field pushes s_i opposite to its neighbours.
			var h float64
			for k, j := range g.Nbr[i] {
				h -= g.Wt[i][k] * float64(s[j])
			}
			ops.SpinUpdates++
			ops.MACs += int64(len(g.Nbr[i]))
			ops.RandomBits++
			p := 1 / (1 + math.Exp(-2*beta*h))
			ns := int8(-1)
			if rng.Float64() < p {
				ns = 1
			}
			if ns != s[i] {
				cut += gain(g, s, i)
				s[i] = ns
				if cut > best {
					best = cut
					copy(bestS, s)
				}
			}
		}
	}
	return CutResult{Spins: bestS, Cut: best, Ops: ops}
}
