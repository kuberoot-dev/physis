package solve

import (
	"os"
	"testing"

	"github.com/kuberoot-dev/physis/pkg/problem"
)

func TestQAOABeatsRandomAndDegradesWithNoise(t *testing.T) {
	var ideal, noisy, rnd float64
	for seed := uint64(1); seed <= 5; seed++ {
		g := problem.RandomGraph(12, 0.4, seed)
		ex := ExactMaxCut(g).Cut
		ideal += QAOA(g, QAOAParams{Layers: 6, Shots: 64}, seed).Cut / ex
		noisy += QAOA(g, QAOAParams{Layers: 6, Shots: 64, TwoQubitError: 0.02}, seed).Cut / ex
		// 64 uniformly random bitstrings: the floor QAOA must beat.
		rnd += QAOA(g, QAOAParams{Layers: 0, Shots: 64}, seed).Cut / ex
	}
	ideal, noisy, rnd = ideal/5, noisy/5, rnd/5
	t.Logf("approximation ratio: ideal %.3f, noisy %.3f, random %.3f", ideal, noisy, rnd)
	if ideal < rnd {
		t.Fatalf("QAOA (%.3f) does not beat random sampling (%.3f)", ideal, rnd)
	}
	if noisy > ideal+1e-9 {
		t.Fatalf("noise improved QAOA: %.3f > %.3f", noisy, ideal)
	}
}

func TestCIMFindsGoodCuts(t *testing.T) {
	var ratio float64
	for seed := uint64(1); seed <= 5; seed++ {
		g := problem.RandomGraph(20, 0.3, seed)
		ratio += CIM(g, 400, 0.05, seed).Cut / ExactMaxCut(g).Cut
	}
	ratio /= 5
	t.Logf("CIM approximation ratio %.3f", ratio)
	if ratio < 0.95 {
		t.Fatalf("CIM ratio %.3f below 0.95", ratio)
	}
}

func digits(t *testing.T) (*problem.Dataset, *problem.Dataset) {
	t.Helper()
	open := func(n string) *problem.Dataset {
		f, err := os.Open("../../data/digits/" + n)
		if err != nil {
			t.Skip("optdigits not fetched: " + err.Error())
		}
		defer f.Close()
		d, err := problem.ReadUCIOptdigits(n, f)
		if err != nil {
			t.Fatal(err)
		}
		return d
	}
	return open("optdigits.tra"), open("optdigits.tes")
}

func TestClassifiersOnDigits(t *testing.T) {
	tr, te := digits(t)
	m := problem.TrainSoftmax(tr, 200, 0.5, 1e-4)
	ex := ClassifyExact(m, te).Accuracy
	an := ClassifyAnalog(m, te, AnalogParams{WeightBits: 4, ProgramNoise: 0.03, ReadNoise: 0.02, ADCBits: 8}, 1).Accuracy
	sp8 := ClassifySpiking(m, te, 8, 1).Accuracy
	sp64 := ClassifySpiking(m, te, 64, 1).Accuracy
	r := NewReservoir(64, 300, 8, 0.05, 1)
	r.Train(tr, 1.0)
	rs := r.Classify(te, 2).Accuracy
	t.Logf("exact %.3f analog %.3f spiking8 %.3f spiking64 %.3f reservoir %.3f", ex, an, sp8, sp64, rs)
	if ex < 0.9 {
		t.Fatalf("reference classifier too weak: %.3f", ex)
	}
	if sp64 < sp8-0.01 {
		t.Fatalf("more timesteps made spiking worse: %.3f < %.3f", sp64, sp8)
	}
	if an > ex+0.02 || rs < 0.5 {
		t.Fatalf("implausible: analog %.3f vs exact %.3f, reservoir %.3f", an, ex, rs)
	}
}
