package solve

import (
	"math"
	"math/cmplx"
	"sort"

	"github.com/kuberoot-dev/physis/pkg/problem"
)

// QAOAParams configure a quantum approximate optimisation run on an emulated
// gate-model quantum processor.
type QAOAParams struct {
	Layers int // p
	Shots  int
	// TwoQubitError is the error per two-qubit gate. The emulator applies a
	// global depolarising approximation: with probability equal to the
	// circuit fidelity (1-e)^(gates) a shot is drawn from the ideal
	// distribution, otherwise it is a uniformly random bitstring. This is
	// crude but standard for estimating how depth erodes quality.
	TwoQubitError float64
}

// QAOAResult extends a cut with the circuit's size.
type QAOAResult struct {
	CutResult
	TwoQubitGates int
	Fidelity      float64
}

// QAOA runs a p-layer QAOA for MaxCut with linear-ramp angles (no classical
// outer optimisation loop, which keeps the run deterministic and the shot
// count honest), on a state-vector simulator, and returns the best cut among
// the sampled shots. Instances above 22 qubits are refused.
func QAOA(g *problem.Graph, p QAOAParams, seed uint64) QAOAResult {
	n := g.N
	if n > 22 {
		panic("QAOA: instance too large for state-vector emulation")
	}
	dim := 1 << uint(n)
	cut := make([]float64, dim)
	for z := 0; z < dim; z++ {
		var c float64
		for _, e := range g.Edges {
			if (z>>uint(e.U))&1 != (z>>uint(e.V))&1 {
				c += e.W
			}
		}
		cut[z] = c
	}
	maxW := 0.0
	for _, e := range g.Edges {
		maxW = math.Max(maxW, math.Abs(e.W))
	}
	if maxW == 0 {
		maxW = 1
	}
	psi := make([]complex128, dim)
	amp := complex(1/math.Sqrt(float64(dim)), 0)
	for i := range psi {
		psi[i] = amp
	}
	// Linear ramp: gamma rises, beta falls, as in a discretised annealing
	// path. The scale 0.7/p per step is a common default for unit weights.
	dt := 0.75
	for l := 0; l < p.Layers; l++ {
		f := (float64(l) + 0.5) / float64(p.Layers)
		gamma := dt * f / maxW
		beta := dt * (1 - f)
		for z := 0; z < dim; z++ {
			psi[z] *= cmplx.Exp(complex(0, -gamma*cut[z]))
		}
		cb, sb := complex(math.Cos(beta), 0), complex(0, -math.Sin(beta))
		for q := 0; q < n; q++ {
			bit := 1 << uint(q)
			for z := 0; z < dim; z++ {
				if z&bit != 0 {
					continue
				}
				a, b := psi[z], psi[z|bit]
				psi[z] = cb*a + sb*b
				psi[z|bit] = sb*a + cb*b
			}
		}
	}
	// Sampling.
	cdf := make([]float64, dim)
	var acc float64
	for z := 0; z < dim; z++ {
		acc += real(psi[z])*real(psi[z]) + imag(psi[z])*imag(psi[z])
		cdf[z] = acc
	}
	gates := p.Layers * len(g.Edges) * 2 // each ZZ term compiles to two CNOTs
	fid := math.Pow(1-p.TwoQubitError, float64(gates))
	rng := rngFor(seed)
	best := -1.0
	bestZ := 0
	for s := 0; s < p.Shots; s++ {
		var z int
		if rng.Float64() < fid {
			z = sort.SearchFloat64s(cdf, rng.Float64()*acc)
			if z >= dim {
				z = dim - 1
			}
		} else {
			z = rng.IntN(dim)
		}
		if cut[z] > best {
			best, bestZ = cut[z], z
		}
	}
	spins := make([]int8, n)
	for i := range spins {
		if (bestZ>>uint(i))&1 == 1 {
			spins[i] = 1
		} else {
			spins[i] = -1
		}
	}
	return QAOAResult{
		CutResult:     CutResult{Spins: spins, Cut: best, Ops: Ops{SpinUpdates: int64(p.Shots), MACs: int64(gates)}},
		TwoQubitGates: gates, Fidelity: fid,
	}
}

// CIM emulates a measurement-feedback coherent Ising machine: optical
// parametric oscillator amplitudes x_i evolve under gain (the pump), loss,
// saturation and the injected feedback field sum_j J_ij x_j, with optical
// noise. Spins are the signs of the amplitudes; the best cut seen along the
// pump ramp is kept. Each round trip updates every spin once.
func CIM(g *problem.Graph, roundTrips int, noise float64, seed uint64) CutResult {
	rng := rngFor(seed)
	n := g.N
	x := make([]float64, n)
	for i := range x {
		x[i] = 0.01 * rng.NormFloat64()
	}
	// Normalise coupling so the feedback field is O(1) regardless of degree.
	var norm float64
	for i := 0; i < n; i++ {
		var s float64
		for _, w := range g.Wt[i] {
			s += math.Abs(w)
		}
		norm = math.Max(norm, s)
	}
	if norm == 0 {
		norm = 1
	}
	eps := 0.5 / norm * 4
	dt := 0.1
	spins := make([]int8, n)
	best := -1.0
	var bestS []int8
	var ops Ops
	h := make([]float64, n)
	for r := 0; r < roundTrips; r++ {
		pump := -0.5 + 2.5*float64(r)/float64(max(roundTrips-1, 1))
		for i := 0; i < n; i++ {
			var f float64
			for k, j := range g.Nbr[i] {
				f -= g.Wt[i][k] * x[j] // antiferromagnetic for MaxCut
			}
			h[i] = f
			ops.MACs += int64(len(g.Nbr[i]))
		}
		for i := 0; i < n; i++ {
			x[i] += dt * ((pump-1-x[i]*x[i])*x[i] + eps*h[i] + noise*rng.NormFloat64())
			ops.SpinUpdates++
		}
		if r%4 == 3 || r == roundTrips-1 {
			for i := range spins {
				if x[i] >= 0 {
					spins[i] = 1
				} else {
					spins[i] = -1
				}
			}
			if c := g.Cut(spins); c > best {
				best = c
				bestS = append(bestS[:0], spins...)
			}
		}
	}
	return CutResult{Spins: bestS, Cut: best, Ops: ops}
}
