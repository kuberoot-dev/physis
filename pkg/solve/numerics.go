package solve

import (
	"math"
	"math/rand/v2"

	"github.com/kuberoot-dev/physis/pkg/problem"
)

// This file holds the classifiers of architectures that change how a
// number is represented or where it is computed, not the physics of the
// switch: low and block-scaled precision (FP8, MXFP4) on a systolic tensor
// array, 8-bit integers in or next to memory and on dataflow processors,
// numbers as random bitstreams (stochastic computing), and hyperdimensional
// vectors. Each runs the arithmetic it would run, so its accuracy is
// measured, not assumed.

// minifloat rounds x to the nearest value of a sign-exponent-mantissa format
// with the given exponent and mantissa widths (no infinities, saturating),
// as FP8 E4M3 and FP4 E2M1 do.
func minifloat(x float64, expBits, manBits int) float64 {
	if x == 0 || math.IsNaN(x) {
		return 0
	}
	bias := (1 << (expBits - 1)) - 1
	maxExp := (1 << expBits) - 1 - bias
	maxVal := math.Ldexp(2-math.Ldexp(1, -manBits), maxExp)
	if expBits == 4 && manBits == 3 {
		maxVal = 448 // E4M3 reserves its top code for NaN
	}
	s := 1.0
	if x < 0 {
		s, x = -1, -x
	}
	if x >= maxVal {
		return s * maxVal
	}
	e := int(math.Floor(math.Log2(x)))
	if e < 1-bias {
		e = 1 - bias // subnormals share the smallest exponent
	}
	step := math.Ldexp(1, e-manBits)
	return s * math.RoundToEven(x/step) * step // ties to even, as the formats specify
}

// mxQuantize quantizes v in blocks of 32 with one shared power-of-two scale
// per block (the OCP Microscaling scheme), each element in the given
// minifloat format.
func mxQuantize(v []float64, expBits, manBits int) []float64 {
	out := make([]float64, len(v))
	bias := (1 << (expBits - 1)) - 1
	maxExp := (1 << expBits) - 1 - bias
	for lo := 0; lo < len(v); lo += 32 {
		hi := min(lo+32, len(v))
		amax := 0.0
		for _, x := range v[lo:hi] {
			amax = math.Max(amax, math.Abs(x))
		}
		scale := 1.0
		if amax > 0 {
			scale = math.Ldexp(1, int(math.Floor(math.Log2(amax)))-maxExp)
		}
		for i := lo; i < hi; i++ {
			out[i] = minifloat(v[i]/scale, expBits, manBits) * scale
		}
	}
	return out
}

func quantizedModel(m *problem.Linear, q func([]float64) []float64) [][]float64 {
	w := make([][]float64, len(m.W))
	for c, row := range m.W {
		w[c] = q(row)
	}
	return w
}

// ClassifyLowPrecision runs the classifier with weights and inputs in a low
// precision format and float32 accumulation, as tensor units do: "fp8"
// (E4M3, block-scaled) or "mxfp4" (E2M1 with MX block scaling). Ops are
// counted as 8-bit-equivalent MACs: an FP4 multiply is charged a quarter of
// an FP8 one (multiplier energy grows roughly with the square of the
// operand width; see the device model for the provenance of that factor).
func ClassifyLowPrecision(m *problem.Linear, d *problem.Dataset, format string) ClassifyResult {
	exp, man, weight := 4, 3, 1.0
	if format == "mxfp4" {
		exp, man, weight = 2, 1, 0.25
	}
	q := func(v []float64) []float64 { return mxQuantize(v, exp, man) }
	w := quantizedModel(m, q)
	correct := 0
	for i, x := range d.X {
		if argmaxScore32(w, m.B, q(x)) == d.Y[i] {
			correct++
		}
	}
	macs := int64(float64(len(d.X)*m.MACs()) * weight)
	return ClassifyResult{Accuracy: float64(correct) / float64(len(d.X)), Correct: correct, Total: len(d.X), Ops: Ops{MACs: macs}}
}

func argmaxScore32(w [][]float64, b []float64, x []float64) int {
	best, bi := float32(math.Inf(-1)), 0
	for c := range w {
		s := float32(b[c])
		for f, v := range x {
			s += float32(w[c][f]) * float32(v)
		}
		if s > best {
			best, bi = s, c
		}
	}
	return bi
}

// ClassifyInt8 runs the classifier in 8-bit integers with 32-bit
// accumulation: weights symmetric per class row, inputs (in 0..1) as
// unsigned bytes. This is what processing-in-memory units and dataflow
// inference processors execute.
func ClassifyInt8(m *problem.Linear, d *problem.Dataset) ClassifyResult {
	wq := make([][]int32, len(m.W))
	ws := make([]float64, len(m.W))
	for c, row := range m.W {
		amax := 0.0
		for _, v := range row {
			amax = math.Max(amax, math.Abs(v))
		}
		ws[c] = amax / 127
		if ws[c] == 0 {
			ws[c] = 1
		}
		wq[c] = make([]int32, len(row))
		for f, v := range row {
			wq[c][f] = int32(math.Round(v / ws[c]))
		}
	}
	correct := 0
	xq := make([]int32, 0, 64)
	for i, x := range d.X {
		xq = xq[:0]
		for _, v := range x {
			xq = append(xq, int32(math.Round(math.Min(math.Max(v, 0), 1)*255)))
		}
		best, bi := math.Inf(-1), 0
		for c := range wq {
			var acc int32
			for f, v := range xq {
				acc += wq[c][f] * v
			}
			s := float64(acc)*ws[c]/255 + m.B[c]
			if s > best {
				best, bi = s, c
			}
		}
		if bi == d.Y[i] {
			correct++
		}
	}
	return ClassifyResult{Accuracy: float64(correct) / float64(len(d.X)), Correct: correct, Total: len(d.X), Ops: Ops{MACs: int64(len(d.X) * m.MACs())}}
}

// ClassifyStochastic runs the classifier in stochastic computing: every
// value in [-1, 1] is a random bitstream whose share of ones encodes it
// (bipolar coding), a multiplication is an XNOR of two streams and a sum is a
// count. Precision grows with the stream length: about 1/sqrt(length). Ops
// are bit operations: length x MACs.
func ClassifyStochastic(m *problem.Linear, d *problem.Dataset, length int, seed uint64) ClassifyResult {
	rng := rngFor(seed)
	scale := make([]float64, len(m.W))
	rowSum := make([]float64, len(m.W))
	for c, row := range m.W {
		for _, v := range row {
			scale[c] = math.Max(scale[c], math.Abs(v))
			rowSum[c] += v
		}
		if scale[c] == 0 {
			scale[c] = 1
		}
	}
	correct := 0
	for i, x := range d.X {
		best, bi := math.Inf(-1), 0
		for c, row := range m.W {
			// sum_f w x = s/2 sum_f w' x' + 1/2 sum_f w, with x' = 2x-1 and
			// w' = w/s in bipolar coding; the first sum is what the streams
			// estimate.
			est := 0.0
			for f, xv := range x {
				pa := math.Min(math.Max(xv, 0), 1) // P(1) of x' = 2x-1
				pb := (row[f]/scale[c] + 1) / 2    // P(1) of w'
				ones := stochasticXNOR(rng, pa, pb, length)
				est += 2*float64(ones)/float64(length) - 1 // estimate of x' w'
			}
			s := scale[c]*est/2 + rowSum[c]/2 + m.B[c]
			if s > best {
				best, bi = s, c
			}
		}
		if bi == d.Y[i] {
			correct++
		}
	}
	bitOps := int64(length) * int64(len(d.X)*m.MACs())
	return ClassifyResult{Accuracy: float64(correct) / float64(len(d.X)), Correct: correct, Total: len(d.X),
		Ops: Ops{MACs: bitOps, RandomBits: 2 * bitOps}}
}

// stochasticXNOR counts the ones of the XNOR of two independent bitstreams
// with the given probabilities of a one. The count is binomial, so it is drawn
// in bulk rather than bit by bit; the result has the same distribution.
func stochasticXNOR(rng *rand.Rand, pa, pb float64, length int) int {
	p := pa*pb + (1-pa)*(1-pb)
	if length <= 64 {
		n := 0
		for k := 0; k < length; k++ {
			if rng.Float64() < p {
				n++
			}
		}
		return n
	}
	// Normal approximation of the binomial for long streams.
	mean := float64(length) * p
	sd := math.Sqrt(float64(length) * p * (1 - p))
	n := int(math.Round(mean + sd*rng.NormFloat64()))
	return min(max(n, 0), length)
}

// HDC is a hyperdimensional classifier: each feature has a random bipolar
// identity vector, each quantised intensity a level vector (neighbouring
// levels share most components), a sample is the sign of the sum of its
// feature-level bindings, and a class is the bundle of its training samples.
// Classification is the nearest class by dot product. Robust to noise and
// made of additions and sign flips, it maps onto in-memory hardware.
type HDC struct {
	D      int
	ids    [][]int8
	levels [][]int8
	proto  [][]int32
}

const hdcLevels = 17

// NewHDC builds and trains a classifier of dimension d on the training set,
// with a few passes of perceptron-style retraining.
func NewHDC(d int, train *problem.Dataset, seed uint64) *HDC {
	rng := rngFor(seed)
	h := &HDC{D: d}
	randVec := func() []int8 {
		v := make([]int8, d)
		for i := range v {
			v[i] = 1
			if rng.IntN(2) == 0 {
				v[i] = -1
			}
		}
		return v
	}
	for f := 0; f < train.Features; f++ {
		h.ids = append(h.ids, randVec())
	}
	// Level vectors: start random, flip d/(2(levels-1)) components per step,
	// so the extremes are nearly orthogonal and neighbours are close.
	lv := randVec()
	perm := rng.Perm(d)
	flip := d / (2 * (hdcLevels - 1))
	for l := 0; l < hdcLevels; l++ {
		h.levels = append(h.levels, append([]int8(nil), lv...))
		for k := l * flip; k < (l+1)*flip && k < d; k++ {
			lv[perm[k]] = -lv[perm[k]]
		}
	}
	enc := make([][]int8, len(train.X))
	for i, x := range train.X {
		enc[i] = h.encode(x, nil)
	}
	h.proto = make([][]int32, train.Classes)
	for c := range h.proto {
		h.proto[c] = make([]int32, d)
	}
	for i, e := range enc {
		add(h.proto[train.Y[i]], e, 1)
	}
	for pass := 0; pass < 5; pass++ {
		for i, e := range enc {
			if p := h.nearest(e, nil); p != train.Y[i] {
				add(h.proto[train.Y[i]], e, 1)
				add(h.proto[p], e, -1)
			}
		}
	}
	return h
}

func add(dst []int32, v []int8, sign int32) {
	for i, x := range v {
		dst[i] += sign * int32(x)
	}
}

func (h *HDC) encode(x []float64, ops *Ops) []int8 {
	sum := make([]int32, h.D)
	for f, v := range x {
		l := int(math.Round(math.Min(math.Max(v, 0), 1) * (hdcLevels - 1)))
		id, lv := h.ids[f], h.levels[l]
		for i := range sum {
			sum[i] += int32(id[i] * lv[i])
		}
	}
	out := make([]int8, h.D)
	for i, s := range sum {
		out[i] = 1
		if s < 0 {
			out[i] = -1
		}
	}
	if ops != nil {
		ops.MACs += int64(h.D * len(x))
	}
	return out
}

func (h *HDC) nearest(e []int8, ops *Ops) int {
	best, bi := int64(math.MinInt64), 0
	for c, p := range h.proto {
		var s int64
		for i, x := range e {
			s += int64(x) * int64(p[i])
		}
		if s > best {
			best, bi = s, c
		}
	}
	if ops != nil {
		ops.MACs += int64(h.D * len(h.proto))
	}
	return bi
}

// Classify runs the trained classifier on a batch.
func (h *HDC) Classify(d *problem.Dataset) ClassifyResult {
	var ops Ops
	correct := 0
	for i, x := range d.X {
		if h.nearest(h.encode(x, &ops), &ops) == d.Y[i] {
			correct++
		}
	}
	return ClassifyResult{Accuracy: float64(correct) / float64(len(d.X)), Correct: correct, Total: len(d.X), Ops: ops}
}
