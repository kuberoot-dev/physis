package solve

import (
	"math"
	"testing"

	"github.com/kuberoot-dev/physis/pkg/problem"
)

func TestMinifloatFormats(t *testing.T) {
	// FP4 E2M1 represents exactly 0, 0.5, 1, 1.5, 2, 3, 4, 6.
	for _, c := range []struct{ in, want float64 }{{0.3, 0.5}, {1.2, 1}, {1.3, 1.5}, {2.4, 2}, {5, 4}, {5.1, 6}, {100, 6}, {-2.6, -3}} {
		if got := minifloat(c.in, 2, 1); got != c.want {
			t.Errorf("fp4(%g) = %g, want %g", c.in, got, c.want)
		}
	}
	// FP8 E4M3 saturates at 448 and keeps 3 mantissa bits.
	if got := minifloat(1000, 4, 3); got != 448 {
		t.Errorf("e4m3 saturation = %g", got)
	}
	if got := minifloat(1.0625, 4, 3); math.Abs(got-1.0) > 1e-12 && math.Abs(got-1.125) > 1e-12 {
		t.Errorf("e4m3(1.0625) = %g", got)
	}
}

// The new classifiers on the real digits: low precision and int8 stay close
// to the exact model; stochastic computing improves with stream length;
// hyperdimensional vectors classify well with enough dimensions.
func TestNumericsClassifiers(t *testing.T) {
	tr, te := digits(t)
	m := problem.TrainSoftmax(tr, 200, 0.5, 1e-4)
	exact := ClassifyExact(m, te).Accuracy
	for _, f := range []string{"fp8", "mxfp4"} {
		a := ClassifyLowPrecision(m, te, f).Accuracy
		t.Logf("%s %.4f (exact %.4f)", f, a, exact)
		if a < exact-0.05 {
			t.Errorf("%s accuracy %.3f far below exact %.3f", f, a, exact)
		}
	}
	if a := ClassifyInt8(m, te).Accuracy; a < exact-0.01 {
		t.Errorf("int8 accuracy %.3f below exact %.3f", a, exact)
	} else {
		t.Logf("int8 %.4f", a)
	}
	short := ClassifyStochastic(m, te, 64, 1).Accuracy
	long := ClassifyStochastic(m, te, 4096, 1).Accuracy
	t.Logf("stochastic 64: %.4f, 4096: %.4f", short, long)
	if long <= short {
		t.Errorf("stochastic computing did not improve with stream length")
	}
	for _, d := range []int{512, 4096} {
		a := NewHDC(d, tr, 1).Classify(te).Accuracy
		t.Logf("hdc %d: %.4f", d, a)
		if d == 4096 && a < 0.85 {
			t.Errorf("hdc %d accuracy %.3f", d, a)
		}
	}
}
