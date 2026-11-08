// Package instances resolves a Problem's InstanceRef: to a shape for the
// scheduler (which never loads the data) and to a runnable instance for an
// agent (which does).
package instances

import (
	"fmt"
	"path/filepath"
	"sync"

	v1 "github.com/kuberoot-dev/physis/api/v1alpha1"
	"github.com/kuberoot-dev/physis/pkg/plan"
	"github.com/kuberoot-dev/physis/pkg/problem"
	"github.com/kuberoot-dev/physis/pkg/run"
	"github.com/kuberoot-dev/physis/pkg/solve"
	"github.com/kuberoot-dev/physis/pkg/workload"
)

// GsetHeaders are the (n, m) of the Gset graphs physis knows.
var GsetHeaders = map[string][2]int{
	"G1": {800, 19176}, "G2": {800, 19176}, "G11": {800, 1600}, "G14": {800, 4694}, "G43": {1000, 9990}, "G22": {2000, 19990},
}

// Shape of an instance, without loading it.
func Shape(class string, ref v1.InstanceRef) (plan.Shape, error) {
	switch {
	case ref.Gset != "":
		h, ok := GsetHeaders[ref.Gset]
		if !ok {
			return plan.Shape{}, fmt.Errorf("unknown Gset instance %q", ref.Gset)
		}
		return plan.Shape{Class: plan.MaxCut, N: h[0], DegreeSum: 2 * h[1]}, nil
	case ref.Random != nil:
		g := problem.RandomGraph(ref.Random.N, ref.Random.P, ref.Random.Seed)
		return plan.Shape{Class: plan.MaxCut, N: g.N, DegreeSum: g.DegreeSum()}, nil
	case ref.Digits != nil:
		return plan.Shape{Class: plan.Classify, N: ref.Digits.Size, DegreeSum: 640}, nil
	}
	return plan.Shape{}, fmt.Errorf("instance not set")
}

// Loader builds runnable instances from a data directory.
type Loader struct {
	Dir string
	mu  sync.Mutex
	dig []workload.Named
	res *solve.Reservoir
	m   *problem.Linear
	tes *problem.Dataset
}

// Load returns a runnable instance. References follow the calibration's
// conventions: published best known for Gset where confirmed, the exact
// optimum for small random graphs, the fp64 accuracy for digits.
func (l *Loader) Load(ref v1.InstanceRef, bestFound map[string]float64) (*run.Instance, error) {
	switch {
	case ref.Gset != "":
		gs, err := workload.Gset(filepath.Join(l.Dir, "gset"), ref.Gset)
		if err != nil {
			return nil, err
		}
		in := gs[0].Instance
		if in.Reference == 0 {
			in.Reference = bestFound[ref.Gset]
		}
		if in.Reference == 0 {
			return nil, fmt.Errorf("no reference cut for %s", ref.Gset)
		}
		return in, nil
	case ref.Random != nil:
		g := problem.RandomGraph(ref.Random.N, ref.Random.P, ref.Random.Seed)
		in := &run.Instance{Graph: g}
		if g.N <= 24 {
			in.Reference = solve.ExactMaxCut(g).Cut
		} else {
			in.Reference = solve.Anneal(g, 30000, 2, 0.02, 1).Cut
		}
		return in, nil
	case ref.Digits != nil:
		l.mu.Lock()
		defer l.mu.Unlock()
		if l.dig == nil {
			d, err := workload.Digits(filepath.Join(l.Dir, "digits"), []int{1797}, 0.3)
			if err != nil {
				return nil, err
			}
			l.dig = d
		}
		full := l.dig[0].Instance
		lo, hi := ref.Digits.Offset, ref.Digits.Offset+ref.Digits.Size
		if lo < 0 || hi > len(full.Batch.X) {
			return nil, fmt.Errorf("digits batch %d..%d outside the test set", lo, hi)
		}
		b := &problem.Dataset{Name: fmt.Sprintf("digits-%d@%d", ref.Digits.Size, lo), X: full.Batch.X[lo:hi], Y: full.Batch.Y[lo:hi], Classes: 10, Features: 64}
		acc := solve.ClassifyExact(full.Model, b).Accuracy
		return &run.Instance{Model: full.Model, Batch: b, ExactAcc: acc, Reservoir: full.Reservoir}, nil
	}
	return nil, fmt.Errorf("instance not set")
}
