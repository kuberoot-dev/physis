package main

import (
	"testing"

	"github.com/kuberoot-dev/physis/pkg/problem"
	"github.com/kuberoot-dev/physis/pkg/run"
)

// The local level raises the effort only on the agent's own evidence: with
// too few runs it trusts the plan; once its runs at the planned effort miss
// the target, it runs the next effort of the same algorithm.
func TestLocalLevelRaisesEffort(t *testing.T) {
	*physics = "ising-digital"
	g := problem.RandomGraph(30, 0.3, 1)
	in := &run.Instance{Graph: g, Reference: 1}
	a := &agent{}
	if e := a.localEffort("anneal", "w300", 0.99, 0.9, in); e != "w300" {
		t.Fatalf("raised without evidence: %s", e)
	}
	for i := 0; i < 10; i++ {
		a.observe("anneal", "w300", in, 0.95)
	}
	e := a.localEffort("anneal", "w300", 0.99, 0.9, in)
	if e == "w300" {
		t.Fatal("did not raise the effort after ten misses")
	}
	// Runs that meet the target leave the plan alone.
	b := &agent{}
	for i := 0; i < 10; i++ {
		b.observe("anneal", "w300", in, 1.0)
	}
	if e := b.localEffort("anneal", "w300", 0.99, 0.9, in); e != "w300" {
		t.Fatalf("raised although the device delivers: %s", e)
	}
}
