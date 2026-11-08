package solve

import (
	"math"

	"github.com/kuberoot-dev/physis/pkg/problem"
)

// ClassifyResult is the outcome of running a classifier over a test set.
type ClassifyResult struct {
	Accuracy float64
	Correct  int
	Total    int
	Ops      Ops
}

// ClassifyExact evaluates the model in float64, the digital reference.
func ClassifyExact(m *problem.Linear, d *problem.Dataset) ClassifyResult {
	var ops Ops
	correct := 0
	for i, x := range d.X {
		if argmaxScore(m.W, m.B, x) == d.Y[i] {
			correct++
		}
		ops.MACs += int64(m.MACs())
	}
	return ClassifyResult{Accuracy: float64(correct) / float64(len(d.X)), Correct: correct, Total: len(d.X), Ops: ops}
}

func argmaxScore(w [][]float64, b []float64, x []float64) int {
	best, bi := math.Inf(-1), 0
	for c := range w {
		s := b[c]
		for f, v := range x {
			s += w[c][f] * v
		}
		if s > best {
			best, bi = s, c
		}
	}
	return bi
}

// AnalogParams describe the non-idealities of an analog in-memory
// matrix-vector multiply. Weights are stored as device conductances with a
// finite number of levels and programming error; every read adds noise.
type AnalogParams struct {
	// WeightBits is the number of conductance levels as bits.
	WeightBits int
	// ProgramNoise is the standard deviation of programming error, relative
	// to the largest weight magnitude.
	ProgramNoise float64
	// ReadNoise is the standard deviation of output noise per MVM, relative
	// to the output range.
	ReadNoise float64
	// ADCBits quantises the output.
	ADCBits int
}

// ClassifyAnalog evaluates the model as an analog crossbar would: weights
// quantised and perturbed once (programming), outputs perturbed on every read
// and quantised by the ADC. The seed fixes one programmed chip.
func ClassifyAnalog(m *problem.Linear, d *problem.Dataset, p AnalogParams, seed uint64) ClassifyResult {
	rng := rngFor(seed)
	wmax := 0.0
	for _, row := range m.W {
		for _, v := range row {
			wmax = math.Max(wmax, math.Abs(v))
		}
	}
	levels := float64(int(1)<<uint(p.WeightBits-1)) - 1
	w := make([][]float64, len(m.W))
	for c, row := range m.W {
		w[c] = make([]float64, len(row))
		for f, v := range row {
			q := math.Round(v/wmax*levels) / levels * wmax
			w[c][f] = q + rng.NormFloat64()*p.ProgramNoise*wmax
		}
	}
	// Output range estimated from the programmed weights: the largest
	// possible |score| for inputs in [0,1].
	orange := 0.0
	for c := range w {
		var s float64
		for _, v := range w[c] {
			s += math.Abs(v)
		}
		orange = math.Max(orange, s+math.Abs(m.B[c]))
	}
	adc := float64(int(1)<<uint(p.ADCBits-1)) - 1
	var ops Ops
	correct := 0
	for i, x := range d.X {
		best, bi := math.Inf(-1), 0
		for c := range w {
			s := m.B[c]
			for f, v := range x {
				s += w[c][f] * v
			}
			s += rng.NormFloat64() * p.ReadNoise * orange
			s = math.Round(s/orange*adc) / adc * orange
			if s > best {
				best, bi = s, c
			}
		}
		if bi == d.Y[i] {
			correct++
		}
		ops.MACs += int64(m.MACs())
	}
	return ClassifyResult{Accuracy: float64(correct) / float64(len(d.X)), Correct: correct, Total: len(d.X), Ops: ops}
}
