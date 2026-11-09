package plan

import (
	"fmt"
	"math"

	"github.com/kuberoot-dev/physis/pkg/device"
	"github.com/kuberoot-dev/physis/pkg/solve"
)

// Class of problem.
type Class string

const (
	MaxCut   Class = "maxcut"
	Classify Class = "classify"
)

// Shape is what the scheduler knows about an instance before running it.
type Shape struct {
	Class Class `json:"class"`
	// N is the number of spins (MaxCut) or of samples (Classify).
	N int `json:"n"`
	// DegreeSum is 2|E| for MaxCut; MACs per sample for Classify.
	DegreeSum int `json:"degreeSum"`
}

// Size is the number used for quality buckets and capacity checks.
func (s Shape) Size() int {
	if s.Class == Classify {
		return s.DegreeSum // weights the substrate must hold
	}
	return s.N
}

// Effort is one setting of an algorithm, ordered from cheap to thorough.
type Effort struct {
	Name   string
	Sweeps int // annealers
	Starts int // local search restarts
}

// Algorithm is a way to solve a class, and the physics it can run on.
type Algorithm struct {
	Name    string
	Class   Class
	Physics []device.Physics
	Efforts []Effort
	// MaxN bounds instances the algorithm accepts (exact enumeration).
	MaxN int
}

// Catalog is what physis can run. The same annealing dynamics appear on the
// CPU (as software) and on an Ising machine (as hardware): the scheduler
// chooses the physics, not just the place.
var Catalog = []Algorithm{
	{Name: "exact", Class: MaxCut, Physics: []device.Physics{device.CPU}, Efforts: []Effort{{Name: "full"}}, MaxN: 24},
	{Name: "local", Class: MaxCut, Physics: []device.Physics{device.CPU},
		Efforts: []Effort{{Name: "s10", Starts: 10}, {Name: "s100", Starts: 100}}},
	{Name: "anneal", Class: MaxCut, Physics: []device.Physics{device.CPU, device.IsingDigital, device.SFQ, device.Adiabatic, device.CGRA},
		Efforts: []Effort{{Name: "w300", Sweeps: 300}, {Name: "w3000", Sweeps: 3000}, {Name: "w30000", Sweeps: 30000}}},
	{Name: "pbit", Class: MaxCut, Physics: []device.Physics{device.PBit},
		Efforts: []Effort{{Name: "w300", Sweeps: 300}, {Name: "w3000", Sweeps: 3000}, {Name: "w30000", Sweeps: 30000}}},
	{Name: "cim", Class: MaxCut, Physics: []device.Physics{device.OpticalIsing},
		Efforts: []Effort{{Name: "r200", Sweeps: 200}, {Name: "r1000", Sweeps: 1000}, {Name: "r5000", Sweeps: 5000}}},
	{Name: "qaoa", Class: MaxCut, Physics: []device.Physics{device.QPU}, MaxN: 20,
		Efforts: []Effort{{Name: "p4s256", Sweeps: 4, Starts: 256}, {Name: "p8s1024", Sweeps: 8, Starts: 1024}}},
	{Name: "exact-fp", Class: Classify, Physics: []device.Physics{device.CPU, device.CGRA}, Efforts: []Effort{{Name: "fp64"}}},
	{Name: "exact-gpu", Class: Classify, Physics: []device.Physics{device.GPU}, Efforts: []Effort{{Name: "fp32"}}},
	{Name: "fp8", Class: Classify, Physics: []device.Physics{device.Systolic}, Efforts: []Effort{{Name: "e4m3"}}},
	{Name: "mxfp4", Class: Classify, Physics: []device.Physics{device.Systolic}, Efforts: []Effort{{Name: "e2m1"}}},
	{Name: "int8", Class: Classify, Physics: []device.Physics{device.Systolic, device.PIMDRAM, device.Dataflow}, Efforts: []Effort{{Name: "int8"}}},
	{Name: "sc", Class: Classify, Physics: []device.Physics{device.Stochastic},
		Efforts: []Effort{{Name: "l64", Sweeps: 64}, {Name: "l256", Sweeps: 256}, {Name: "l1024", Sweeps: 1024}, {Name: "l4096", Sweeps: 4096}}},
	{Name: "hdc", Class: Classify, Physics: []device.Physics{device.HDC},
		Efforts: []Effort{{Name: "d512", Sweeps: 512}, {Name: "d2048", Sweeps: 2048}, {Name: "d8192", Sweeps: 8192}}},
	{Name: "analog", Class: Classify, Physics: []device.Physics{device.AnalogIMC, device.Photonic}, Efforts: []Effort{{Name: "w4"}}},
	{Name: "spiking", Class: Classify, Physics: []device.Physics{device.Neuromorphic},
		Efforts: []Effort{{Name: "t8", Sweeps: 8}, {Name: "t32", Sweeps: 32}, {Name: "t128", Sweeps: 128}}},
	{Name: "reservoir", Class: Classify, Physics: []device.Physics{device.Organoid},
		Efforts: []Effort{{Name: "s8", Sweeps: 8}, {Name: "s20", Sweeps: 20}}},
}

// Find returns an algorithm by name.
func Find(name string) (Algorithm, error) {
	for _, a := range Catalog {
		if a.Name == name {
			return a, nil
		}
	}
	return Algorithm{}, fmt.Errorf("unknown algorithm %q", name)
}

// EstimateOps returns the operation count of a run before it happens. For
// annealers and the classifier the count is exact: it depends only on the
// shape and the effort. Local search stops when no flip improves the cut, so
// its count is an estimate, corrected from receipts.
func EstimateOps(a Algorithm, e Effort, s Shape) (solve.Ops, bool) {
	switch a.Name {
	case "exact":
		if s.N > a.MaxN {
			return solve.Ops{}, false
		}
		u := int64(1) << uint(s.N-1)
		return solve.Ops{SpinUpdates: u, MACs: u * int64(s.DegreeSum) / int64(max(s.N, 1))}, true
	case "local":
		// About four passes per restart until no flip helps (measured on
		// Gset and random graphs; corrected online).
		passes := int64(4)
		u := int64(e.Starts) * passes * int64(s.N)
		return solve.Ops{SpinUpdates: u, MACs: int64(e.Starts) * passes * int64(s.DegreeSum)}, true
	case "anneal":
		return solve.Ops{SpinUpdates: int64(e.Sweeps) * int64(s.N), MACs: int64(e.Sweeps) * int64(s.DegreeSum), RandomBits: 32 * int64(e.Sweeps) * int64(s.N)}, true
	case "pbit":
		return solve.Ops{SpinUpdates: int64(e.Sweeps) * int64(s.N), MACs: int64(e.Sweeps) * int64(s.DegreeSum), RandomBits: int64(e.Sweeps) * int64(s.N)}, true
	case "exact-fp", "exact-gpu", "analog", "fp8", "int8":
		return solve.Ops{MACs: int64(s.N) * int64(s.DegreeSum)}, true
	case "mxfp4":
		// Charged as 8-bit-equivalent MACs: a quarter of an FP8 multiply.
		return solve.Ops{MACs: int64(s.N) * int64(s.DegreeSum) / 4}, true
	case "sc":
		// Bit operations: stream length x MACs.
		return solve.Ops{MACs: int64(e.Sweeps) * int64(s.N) * int64(s.DegreeSum), RandomBits: 2 * int64(e.Sweeps) * int64(s.N) * int64(s.DegreeSum)}, true
	case "hdc":
		// Encoding (dimension x features) plus similarity (dimension x
		// classes) per sample; DegreeSum carries classes x features = 640.
		return solve.Ops{MACs: int64(s.N) * int64(e.Sweeps) * int64(s.DegreeSum/10+10)}, true
	case "cim":
		return solve.Ops{SpinUpdates: int64(e.Sweeps) * int64(s.N), MACs: int64(e.Sweeps) * int64(s.DegreeSum)}, true
	case "qaoa":
		if s.N > a.MaxN {
			return solve.Ops{}, false
		}
		// Shots are the QPU's operation; gates are recorded for the error model.
		return solve.Ops{SpinUpdates: int64(e.Starts), MACs: int64(e.Sweeps) * int64(s.DegreeSum)}, true
	case "spiking":
		// Expected synaptic events: steps x samples x (mean active inputs
		// ~ 30% of 64 for digits) x classes. DegreeSum carries MACs per
		// sample = classes x features, so events ~ 0.3 x steps x MACs.
		return solve.Ops{MACs: int64(float64(e.Sweeps) * 0.3 * float64(s.N) * float64(s.DegreeSum))}, true
	case "reservoir":
		// Unit-steps: samples x steps x units (300).
		return solve.Ops{SpinUpdates: int64(s.N) * int64(e.Sweeps) * 300}, true
	}
	return solve.Ops{}, false
}

// CanRun says whether a substrate holds a shape.
func CanRun(m *device.Model, a Algorithm, s Shape) bool {
	ok := false
	for _, p := range a.Physics {
		if p == m.Physics {
			ok = true
		}
	}
	if !ok || a.Class != s.Class {
		return false
	}
	if a.MaxN > 0 && s.N > a.MaxN {
		return false
	}
	return s.Size() <= m.MaxSize
}

func finite(x float64) bool { return !math.IsNaN(x) && !math.IsInf(x, 0) }
