package solve

import (
	"math"

	"github.com/kuberoot-dev/physis/pkg/problem"
)

// ClassifySpiking runs the linear classifier as a spiking network would on a
// neuromorphic chip: each input pixel fires a Bernoulli spike per timestep
// with probability equal to its intensity, every spike delivers its weights
// to the output neurons (one synaptic event per weight), and the class whose
// neuron integrated the most charge wins. More timesteps average out the
// sampling noise: quality is bought with events, which is exactly what the
// chip charges for.
func ClassifySpiking(m *problem.Linear, d *problem.Dataset, steps int, seed uint64) ClassifyResult {
	rng := rngFor(seed)
	var ops Ops
	correct := 0
	v := make([]float64, len(m.W))
	for i, x := range d.X {
		for c := range v {
			v[c] = m.B[c] * float64(steps)
		}
		for t := 0; t < steps; t++ {
			for f, p := range x {
				if p > 0 && rng.Float64() < p {
					for c := range m.W {
						v[c] += m.W[c][f]
					}
					ops.MACs += int64(len(m.W)) // synaptic events
					ops.RandomBits++
				}
			}
		}
		best, bi := math.Inf(-1), 0
		for c, s := range v {
			if s > best {
				best, bi = s, c
			}
		}
		if bi == d.Y[i] {
			correct++
		}
	}
	return ClassifyResult{Accuracy: float64(correct) / float64(len(d.X)), Correct: correct, Total: len(d.X), Ops: ops}
}

// Reservoir is a fixed random recurrent network driven by the input, with a
// linear readout trained on its states: reservoir computing, the way living
// neuronal cultures are used for computation today. The reservoir itself is
// never trained (one does not train a culture's synapses by gradient), only
// the readout, which runs on a conventional computer.
type Reservoir struct {
	Units int
	Steps int     // stimulation steps per sample
	Leak  float64 // leaky integration rate
	Noise float64 // intrinsic noise of the culture, relative
	win   [][]float64
	wres  [][]int
	wresW [][]float64
	readW [][]float64
	readB []float64
	seed  uint64
}

// NewReservoir builds a sparse random network with spectral radius below one
// (an echo-state network), seeded so the "culture" is the same on every run.
func NewReservoir(inputs, units, steps int, noise float64, seed uint64) *Reservoir {
	rng := rngFor(seed)
	r := &Reservoir{Units: units, Steps: steps, Leak: 0.5, Noise: noise, seed: seed}
	r.win = make([][]float64, units)
	for u := range r.win {
		r.win[u] = make([]float64, inputs)
		for f := range r.win[u] {
			if rng.Float64() < 0.2 {
				r.win[u][f] = rng.NormFloat64()
			}
		}
	}
	const fanIn = 10
	scale := 0.9 / math.Sqrt(fanIn)
	r.wres = make([][]int, units)
	r.wresW = make([][]float64, units)
	for u := 0; u < units; u++ {
		for k := 0; k < fanIn; k++ {
			r.wres[u] = append(r.wres[u], rng.IntN(units))
			r.wresW[u] = append(r.wresW[u], rng.NormFloat64()*scale)
		}
	}
	return r
}

// state drives the reservoir with one sample and returns its final state,
// counting synaptic events.
func (r *Reservoir) state(x []float64, rng interface{ NormFloat64() float64 }, ops *Ops) []float64 {
	s := make([]float64, r.Units)
	ns := make([]float64, r.Units)
	for t := 0; t < r.Steps; t++ {
		for u := 0; u < r.Units; u++ {
			var a float64
			for f, v := range x {
				if w := r.win[u][f]; w != 0 && v != 0 {
					a += w * v
					ops.MACs++
				}
			}
			for k, j := range r.wres[u] {
				a += r.wresW[u][k] * s[j]
			}
			ops.MACs += int64(len(r.wres[u]))
			a += r.Noise * rng.NormFloat64()
			ns[u] = (1-r.Leak)*s[u] + r.Leak*math.Tanh(a)
		}
		s, ns = ns, s
		ops.SpinUpdates += int64(r.Units)
	}
	return s
}

// Train fits the readout by ridge regression on one-hot targets, solved in
// closed form through the normal equations (Cholesky). Training stimulates
// the culture with every training sample; its ops are returned.
func (r *Reservoir) Train(d *problem.Dataset, lambda float64) Ops {
	rng := rngFor(r.seed + 1)
	var ops Ops
	k := r.Units + 1 // states plus a bias term
	a := make([][]float64, k)
	for i := range a {
		a[i] = make([]float64, k)
	}
	b := make([][]float64, d.Classes)
	for c := range b {
		b[c] = make([]float64, k)
	}
	z := make([]float64, k)
	for i, x := range d.X {
		s := r.state(x, rng, &ops)
		copy(z, s)
		z[r.Units] = 1
		for p := 0; p < k; p++ {
			for q := 0; q <= p; q++ {
				a[p][q] += z[p] * z[q]
			}
			b[d.Y[i]][p] += z[p]
		}
	}
	for p := 0; p < k; p++ {
		for q := 0; q < p; q++ {
			a[q][p] = a[p][q]
		}
		a[p][p] += lambda
	}
	l := cholesky(a)
	r.readW = make([][]float64, d.Classes)
	r.readB = make([]float64, d.Classes)
	for c := 0; c < d.Classes; c++ {
		w := cholSolve(l, b[c])
		r.readW[c] = w[:r.Units]
		r.readB[c] = w[r.Units]
	}
	return ops
}

func cholesky(a [][]float64) [][]float64 {
	n := len(a)
	l := make([][]float64, n)
	for i := range l {
		l[i] = make([]float64, n)
	}
	for i := 0; i < n; i++ {
		for j := 0; j <= i; j++ {
			s := a[i][j]
			for k := 0; k < j; k++ {
				s -= l[i][k] * l[j][k]
			}
			if i == j {
				l[i][i] = math.Sqrt(math.Max(s, 1e-12))
			} else {
				l[i][j] = s / l[j][j]
			}
		}
	}
	return l
}

func cholSolve(l [][]float64, b []float64) []float64 {
	n := len(l)
	y := make([]float64, n)
	for i := 0; i < n; i++ {
		s := b[i]
		for k := 0; k < i; k++ {
			s -= l[i][k] * y[k]
		}
		y[i] = s / l[i][i]
	}
	x := make([]float64, n)
	for i := n - 1; i >= 0; i-- {
		s := y[i]
		for k := i + 1; k < n; k++ {
			s -= l[k][i] * x[k]
		}
		x[i] = s / l[i][i]
	}
	return x
}

// Classify runs the test set through the culture and the trained readout.
func (r *Reservoir) Classify(d *problem.Dataset, seed uint64) ClassifyResult {
	rng := rngFor(seed)
	var ops Ops
	correct := 0
	for i, x := range d.X {
		s := r.state(x, rng, &ops)
		best, bi := math.Inf(-1), 0
		for c := range r.readW {
			p := r.readB[c]
			for u, v := range s {
				p += r.readW[c][u] * v
			}
			if p > best {
				best, bi = p, c
			}
		}
		if bi == d.Y[i] {
			correct++
		}
	}
	return ClassifyResult{Accuracy: float64(correct) / float64(len(d.X)), Correct: correct, Total: len(d.X), Ops: ops}
}
