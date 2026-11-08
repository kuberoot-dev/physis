// Package workload loads the instances the experiments use: Gset MaxCut
// graphs, small random graphs with exact optima, and batches of the UCI
// handwritten digits with a trained linear model and a trained reservoir.
package workload

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/kuberoot-dev/physis/pkg/problem"
	"github.com/kuberoot-dev/physis/pkg/run"
	"github.com/kuberoot-dev/physis/pkg/solve"
)

// Named is an instance with a name.
type Named struct {
	Name string
	*run.Instance
	// RefSource says where the reference value comes from.
	RefSource string
}

// Gset loads the named Gset graphs from dir. Reference values come from
// problem.GsetBestKnown when confirmed, otherwise they are filled in later
// from the best cut found.
func Gset(dir string, names ...string) ([]Named, error) {
	var out []Named
	for _, n := range names {
		f, err := os.Open(filepath.Join(dir, n))
		if err != nil {
			return nil, err
		}
		g, err := problem.ReadGset(n, f)
		f.Close()
		if err != nil {
			return nil, err
		}
		in := Named{Name: n, Instance: &run.Instance{Graph: g}}
		if bk, ok := problem.GsetBestKnown[n]; ok {
			in.Reference, in.RefSource = bk, "best-known (published)"
		}
		out = append(out, in)
	}
	return out, nil
}

// SmallRandom returns G(n, p) graphs with their exact optimum as reference.
func SmallRandom(sizes []int, p float64, perSize int) []Named {
	var out []Named
	for _, n := range sizes {
		for s := 0; s < perSize; s++ {
			g := problem.RandomGraph(n, p, uint64(1000*n+s))
			out = append(out, Named{Name: g.Name, Instance: &run.Instance{Graph: g, Reference: solve.ExactMaxCut(g).Cut}, RefSource: "exact"})
		}
	}
	return out
}

// Digits trains the reference model and reservoir on optdigits.tra and cuts
// optdigits.tes into batches of the given sizes.
func Digits(dir string, sizes []int, reservoirNoise float64) ([]Named, error) {
	open := func(n string) (*problem.Dataset, error) {
		f, err := os.Open(filepath.Join(dir, n))
		if err != nil {
			return nil, err
		}
		defer f.Close()
		return problem.ReadUCIOptdigits(n, f)
	}
	tr, err := open("optdigits.tra")
	if err != nil {
		return nil, err
	}
	te, err := open("optdigits.tes")
	if err != nil {
		return nil, err
	}
	m := problem.TrainSoftmax(tr, 200, 0.5, 1e-4)
	res := solve.NewReservoir(64, 300, 8, reservoirNoise, 1)
	res.Train(tr, 1.0)
	var out []Named
	for _, sz := range sizes {
		for off := 0; off+sz <= len(te.X) && len(out) < 1000; off += sz {
			b := &problem.Dataset{Name: fmt.Sprintf("digits-%d@%d", sz, off), X: te.X[off : off+sz], Y: te.Y[off : off+sz], Classes: 10, Features: 64}
			acc := solve.ClassifyExact(m, b).Accuracy
			out = append(out, Named{Name: b.Name, Instance: &run.Instance{Model: m, Batch: b, ExactAcc: acc, Reservoir: res}, RefSource: "exact fp64"})
			if sz >= len(te.X)/2 {
				break
			}
		}
	}
	return out, nil
}
