package main

import (
	"context"
	"slices"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"

	v1 "github.com/kuberoot-dev/physis/api/v1alpha1"
	"github.com/kuberoot-dev/physis/pkg/kube"
	"github.com/kuberoot-dev/physis/pkg/kube/kubefake"
	"github.com/kuberoot-dev/physis/pkg/reason"
)

var t0 = time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)

func problem(ns, name, requestID string, created time.Time) *kube.Problem {
	return &kube.Problem{
		Meta: metav1.ObjectMeta{Namespace: ns, Name: name, UID: types.UID(ns + "/" + name), CreationTimestamp: metav1.NewTime(created)},
		Spec: v1.ProblemSpec{Class: "classify", Instance: v1.InstanceRef{Digits: &v1.DigitsBatch{Size: 100}}, Quality: 0.9, Policy: "min-energy", RequestID: requestID},
	}
}

func fakeScheduler(t *testing.T, probs ...*kube.Problem) *scheduler {
	t.Helper()
	var objs []runtime.Object
	for _, p := range probs {
		u, err := kube.Encode("Problem", p)
		if err != nil {
			t.Fatal(err)
		}
		objs = append(objs, u)
	}
	c, _ := kubefake.New(objs...)
	return &scheduler{c: c, seen: map[string]bool{}, limit: newRateLimiter(0, time.Minute)}
}

// An agent that times out and resubmits the same request under another name
// must not get the work twice: the copy is rejected with a code and a
// pointer to the original, and the original is planned.
func TestDuplicateRequestIsRejectedWithAPointerToTheOriginal(t *testing.T) {
	orig := problem("agents", "solve-1", "req-42", t0)
	retry := problem("agents", "solve-1-retry", "req-42", t0.Add(3*time.Second))
	other := problem("agents", "solve-2", "req-43", t0.Add(time.Second))
	elsewhere := problem("other-team", "solve-1", "req-42", t0.Add(-time.Hour))
	all := []*kube.Problem{retry, other, orig, elsewhere}
	s := fakeScheduler(t, all...)
	ctx := context.Background()

	if !s.admit(ctx, orig, all, t0) {
		t.Fatalf("the original was not admitted: %+v", orig.Status)
	}
	if !s.admit(ctx, other, all, t0) {
		t.Fatalf("a different requestID was not admitted: %+v", other.Status)
	}
	if s.admit(ctx, retry, all, t0) {
		t.Fatal("the duplicate was admitted")
	}
	got, err := s.c.GetProblem(ctx, "agents", "solve-1-retry")
	if err != nil {
		t.Fatal(err)
	}
	st := got.Status
	if st.Phase != v1.PhaseRejected || st.DuplicateOf != "solve-1" || !slices.Equal(st.ReasonCodes, []string{reason.Duplicate}) || st.Message == "" {
		t.Fatalf("duplicate status: %+v", st)
	}
	// The same requestID in another namespace is another request.
	if !s.admit(ctx, elsewhere, all, t0) {
		t.Fatalf("a requestID was shared across namespaces: %+v", elsewhere.Status)
	}
}

func TestDryRunsAndRefusedRetriesDoNotHoldARequestID(t *testing.T) {
	dry := problem("agents", "a-dry", "req-1", t0)
	dry.Spec.DryRun = true
	limited := problem("agents", "b-limited", "req-1", t0.Add(time.Second))
	limited.Status = v1.ProblemStatus{Phase: v1.PhaseRejected, ReasonCodes: []string{reason.RateLimited}}
	real := problem("agents", "c-real", "req-1", t0.Add(2*time.Second))
	all := []*kube.Problem{dry, limited, real}
	if d := duplicateOf(real, all); d != "" {
		t.Fatalf("held by %s; dry runs and rate-limited retries must not hold a requestID", d)
	}
	if d := duplicateOf(dry, all); d != "" {
		t.Fatalf("a dry run was rejected as a duplicate of %s", d)
	}
	// Equal timestamps: the name decides, the same way on every replica.
	x, y := problem("agents", "x", "req-2", t0), problem("agents", "y", "req-2", t0)
	if duplicateOf(y, []*kube.Problem{x, y}) != "x" || duplicateOf(x, []*kube.Problem{x, y}) != "" {
		t.Fatal("ties are not broken by name")
	}
}

func TestRateLimitRejectsTheExcessPerNamespace(t *testing.T) {
	var probs []*kube.Problem
	for i := 0; i < 5; i++ {
		probs = append(probs, problem("agents", string(rune('a'+i)), "", t0))
	}
	quiet := problem("quiet", "q", "", t0)
	s := fakeScheduler(t, append(probs, quiet)...)
	s.limit = newRateLimiter(3, time.Minute)
	ctx := context.Background()
	admitted := 0
	for _, p := range probs {
		if s.admit(ctx, p, probs, t0) {
			admitted++
		}
	}
	if admitted != 3 {
		t.Fatalf("admitted %d of 5 with a limit of 3", admitted)
	}
	got, err := s.c.GetProblem(ctx, "agents", "e")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status.Phase != v1.PhaseRejected || !slices.Equal(got.Status.ReasonCodes, []string{reason.RateLimited}) {
		t.Fatalf("excess status: %+v", got.Status)
	}
	if !s.admit(ctx, quiet, nil, t0) {
		t.Fatal("one namespace's burst limited another")
	}
	// A problem seen again is not counted again, and work returned to the
	// pool is not new work.
	if !s.admit(ctx, probs[0], probs, t0.Add(time.Second)) {
		t.Fatal("an admitted problem was counted twice")
	}
	back := problem("agents", "replanned", "", t0)
	back.Status.Phase = v1.PhasePending
	if !s.admit(ctx, back, nil, t0.Add(time.Second)) {
		t.Fatal("work returned to the pool was rate limited")
	}
	// The window slides.
	if !s.admit(ctx, problem("agents", "later", "", t0.Add(time.Minute)), nil, t0.Add(time.Minute)) {
		t.Fatal("the window did not slide")
	}
}
