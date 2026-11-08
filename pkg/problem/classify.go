package problem

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
)

// Dataset is a labelled set of feature vectors.
type Dataset struct {
	Name     string
	X        [][]float64
	Y        []int
	Classes  int
	Features int
}

// ReadUCIOptdigits parses the UCI "Optical Recognition of Handwritten Digits"
// files (optdigits.tra / optdigits.tes): 64 integer features in 0..16 and a
// class label in 0..9 per line, comma separated. Features are scaled to 0..1.
func ReadUCIOptdigits(name string, r io.Reader) (*Dataset, error) {
	d := &Dataset{Name: name, Classes: 10, Features: 64}
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		parts := strings.Split(line, ",")
		if len(parts) != 65 {
			return nil, fmt.Errorf("%s: %d fields, want 65", name, len(parts))
		}
		x := make([]float64, 64)
		for i := 0; i < 64; i++ {
			v, err := strconv.Atoi(parts[i])
			if err != nil {
				return nil, err
			}
			x[i] = float64(v) / 16
		}
		y, err := strconv.Atoi(parts[64])
		if err != nil {
			return nil, err
		}
		d.X = append(d.X, x)
		d.Y = append(d.Y, y)
	}
	return d, sc.Err()
}

// Digest hashes the dataset content for receipts.
func (d *Dataset) Digest() string {
	h := sha256.New()
	for i, x := range d.X {
		for _, v := range x {
			h.Write([]byte(strconv.FormatFloat(v, 'g', -1, 64)))
		}
		h.Write([]byte{byte(d.Y[i])})
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}

// Linear is a multinomial logistic-regression model: scores = W x + b.
type Linear struct {
	W [][]float64 // Classes x Features
	B []float64
}

// TrainSoftmax fits a linear classifier by full-batch gradient descent with
// L2 regularisation. It is deterministic, which keeps the exact reference the
// same on every run.
func TrainSoftmax(d *Dataset, epochs int, lr, l2 float64) *Linear {
	m := &Linear{W: make([][]float64, d.Classes), B: make([]float64, d.Classes)}
	for c := range m.W {
		m.W[c] = make([]float64, d.Features)
	}
	n := float64(len(d.X))
	gw := make([][]float64, d.Classes)
	for c := range gw {
		gw[c] = make([]float64, d.Features)
	}
	gb := make([]float64, d.Classes)
	p := make([]float64, d.Classes)
	for ep := 0; ep < epochs; ep++ {
		for c := range gw {
			clear(gw[c])
		}
		clear(gb)
		for i, x := range d.X {
			m.softmax(x, p)
			for c := 0; c < d.Classes; c++ {
				g := p[c]
				if c == d.Y[i] {
					g -= 1
				}
				gb[c] += g
				for f, v := range x {
					gw[c][f] += g * v
				}
			}
		}
		for c := 0; c < d.Classes; c++ {
			m.B[c] -= lr * gb[c] / n
			for f := range m.W[c] {
				m.W[c][f] -= lr * (gw[c][f]/n + l2*m.W[c][f])
			}
		}
	}
	return m
}

func (m *Linear) softmax(x, out []float64) {
	mx := math.Inf(-1)
	for c := range m.W {
		s := m.B[c]
		for f, v := range x {
			s += m.W[c][f] * v
		}
		out[c] = s
		if s > mx {
			mx = s
		}
	}
	var z float64
	for c := range out {
		out[c] = math.Exp(out[c] - mx)
		z += out[c]
	}
	for c := range out {
		out[c] /= z
	}
}

// MACs is the number of multiply-accumulate operations one inference costs.
func (m *Linear) MACs() int { return len(m.W) * len(m.W[0]) }
