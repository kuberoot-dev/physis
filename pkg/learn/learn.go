// Package learn keeps what physis has observed: the distribution of result
// quality per algorithm, effort and physics, and correction factors between
// what a model predicted and what a run measured. The scheduler predicts from
// these; every receipt feeds them. This is the feedback loop that turns fixed
// device models into models that track the hardware they describe.
package learn

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"sort"
	"sync"
)

// Key identifies a quality distribution.
type Key struct {
	Class   string `json:"class"`
	Algo    string `json:"algo"`
	Effort  string `json:"effort"`
	Physics string `json:"physics"`
	// SizeBucket groups instances of similar size: floor(log2(size)).
	SizeBucket int `json:"sizeBucket"`
}

func (k Key) String() string {
	return fmt.Sprintf("%s/%s/%s/%s/b%d", k.Class, k.Algo, k.Effort, k.Physics, k.SizeBucket)
}

// Bucket of a problem size.
func Bucket(size int) int {
	if size < 1 {
		return 0
	}
	return int(math.Floor(math.Log2(float64(size))))
}

// Store is safe for concurrent use.
type Store struct {
	mu sync.Mutex
	// Quality samples, normalised to [0,1] (cut / reference, accuracy).
	Quality map[string][]float64 `json:"quality"`
	// Correction factors measured/predicted, per substrate and quantity.
	Correction map[string]*EWMA `json:"correction"`
}

func NewStore() *Store {
	return &Store{Quality: map[string][]float64{}, Correction: map[string]*EWMA{}}
}

// Observe records one quality sample.
func (s *Store) Observe(k Key, q float64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Quality[k.String()] = append(s.Quality[k.String()], q)
}

// Samples returns a copy of the samples for a key.
func (s *Store) Samples(k Key) []float64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]float64(nil), s.Quality[k.String()]...)
}

// ProbAtLeast estimates P(quality >= target) from samples with a Wilson
// lower bound at the given z, so that a handful of lucky runs does not make
// an effort level look reliable. With no samples it returns 0: physis does
// not promise what it has never measured.
func (s *Store) ProbAtLeast(k Key, target, z float64) (p float64, n int) {
	xs := s.Samples(k)
	n = len(xs)
	if n == 0 {
		return 0, 0
	}
	hits := 0
	for _, x := range xs {
		if x >= target-1e-12 {
			hits++
		}
	}
	return wilsonLower(float64(hits), float64(n), z), n
}

// Mean quality of a key, NaN without samples.
func (s *Store) Mean(k Key) float64 {
	xs := s.Samples(k)
	if len(xs) == 0 {
		return math.NaN()
	}
	var t float64
	for _, x := range xs {
		t += x
	}
	return t / float64(len(xs))
}

func wilsonLower(k, n, z float64) float64 {
	if n == 0 {
		return 0
	}
	p := k / n
	den := 1 + z*z/n
	c := p + z*z/(2*n)
	m := z * math.Sqrt(p*(1-p)/n+z*z/(4*n*n))
	return math.Max(0, (c-m)/den)
}

// EWMA is an exponentially weighted mean of measured/predicted ratios.
type EWMA struct {
	Value float64 `json:"value"`
	N     int     `json:"n"`
	// Alpha weights the newest observation.
	Alpha float64 `json:"alpha"`
}

// Update folds in a new ratio. The first observation replaces the prior.
func (e *EWMA) Update(x float64) {
	if math.IsNaN(x) || math.IsInf(x, 0) || x <= 0 {
		return
	}
	if e.N == 0 {
		e.Value = x
	} else {
		e.Value = e.Alpha*x + (1-e.Alpha)*e.Value
	}
	e.N++
}

// Correct returns the correction factor for a substrate quantity (1 when
// nothing has been measured).
func (s *Store) Correct(substrate, quantity string) float64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e, ok := s.Correction[substrate+"/"+quantity]; ok && e.N > 0 {
		return e.Value
	}
	return 1
}

// Measure records measured/predicted for a substrate quantity.
func (s *Store) Measure(substrate, quantity string, measured, predicted float64) {
	if predicted <= 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	k := substrate + "/" + quantity
	e, ok := s.Correction[k]
	if !ok {
		e = &EWMA{Alpha: 0.2}
		s.Correction[k] = e
	}
	e.Update(measured / predicted)
}

// Save writes the store as JSON.
func (s *Store) Save(w io.Writer) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	enc := json.NewEncoder(w)
	enc.SetIndent("", " ")
	return enc.Encode(s)
}

// Load reads a store saved by Save.
func Load(r io.Reader) (*Store, error) {
	s := NewStore()
	if err := json.NewDecoder(r).Decode(s); err != nil {
		return nil, err
	}
	if s.Quality == nil {
		s.Quality = map[string][]float64{}
	}
	if s.Correction == nil {
		s.Correction = map[string]*EWMA{}
	}
	return s, nil
}

// Keys lists the recorded quality keys.
func (s *Store) Keys() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var ks []string
	for k := range s.Quality {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}
