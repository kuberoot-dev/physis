package main

import (
	"context"
	"fmt"
	"slices"
	"time"

	v1 "github.com/kuberoot-dev/physis/api/v1alpha1"
	"github.com/kuberoot-dev/physis/pkg/kube"
	"github.com/kuberoot-dev/physis/pkg/reason"
)

// admit runs the checks that come before planning: idempotency and the
// namespace's rate. It reports whether the problem may be planned; when it
// may not, the problem has been rejected with a reason code.
func (s *scheduler) admit(ctx context.Context, p *kube.Problem, all []*kube.Problem, now time.Time) bool {
	if orig := duplicateOf(p, all); orig != "" {
		p.Status.DuplicateOf = orig
		s.reject(ctx, p, fmt.Sprintf("duplicate of %s: same requestID %q", orig, p.Spec.RequestID), nil, []string{reason.Duplicate})
		return false
	}
	// Only new problems count against the rate: work returned to the pool
	// was admitted once already.
	if p.Status.Phase == "" && !s.limit.allow(p.Meta.Namespace, string(p.Meta.UID), now) {
		s.reject(ctx, p, fmt.Sprintf("rate limited: namespace %s submitted more than %d new problems in %s; retry later under a new name",
			p.Meta.Namespace, s.limit.limit, s.limit.window), nil, []string{reason.RateLimited})
		return false
	}
	return true
}

// holdsRequestID reports whether q claims its spec.requestID. A dry run
// runs nothing, so it has nothing to protect; a problem refused as a
// duplicate or for the rate never ran, so a retry must be able to take its
// place.
func holdsRequestID(q *kube.Problem) bool {
	if q.Spec.RequestID == "" || q.Spec.DryRun {
		return false
	}
	if q.Status.Phase == v1.PhaseRejected &&
		(slices.Contains(q.Status.ReasonCodes, reason.Duplicate) || slices.Contains(q.Status.ReasonCodes, reason.RateLimited)) {
		return false
	}
	return true
}

// duplicateOf returns the name of the problem that holds p's requestID in
// p's namespace when that is not p itself, and "" otherwise. The holder is
// the earliest created of the problems that hold the ID, so every scheduler
// replica and every restart picks the same one.
func duplicateOf(p *kube.Problem, all []*kube.Problem) string {
	if !holdsRequestID(p) {
		return ""
	}
	first := p
	for _, q := range all {
		if q.Meta.Namespace != p.Meta.Namespace || q.Meta.Name == p.Meta.Name || q.Spec.RequestID != p.Spec.RequestID || !holdsRequestID(q) {
			continue
		}
		if createdBefore(q, first) {
			first = q
		}
	}
	if first == p {
		return ""
	}
	return first.Meta.Name
}

// createdBefore orders problems by creation time, then by name: creation
// timestamps have a resolution of one second.
func createdBefore(a, b *kube.Problem) bool {
	ta, tb := a.Meta.CreationTimestamp.Time, b.Meta.CreationTimestamp.Time
	if !ta.Equal(tb) {
		return ta.Before(tb)
	}
	if a.Meta.Namespace != b.Meta.Namespace {
		return a.Meta.Namespace < b.Meta.Namespace
	}
	return a.Meta.Name < b.Meta.Name
}

// rateLimiter admits at most limit new problems per namespace in any
// sliding window. It lives in the scheduler's memory: a restart forgets the
// window, which errs on the side of admitting.
type rateLimiter struct {
	limit    int
	window   time.Duration
	admitted map[string][]admission
}

type admission struct {
	uid string
	at  time.Time
}

func newRateLimiter(limit int, window time.Duration) *rateLimiter {
	return &rateLimiter{limit: limit, window: window, admitted: map[string][]admission{}}
}

// allow records and admits a new problem of namespace ns, or refuses it when
// the namespace already had limit admissions in the window. A problem seen
// again (its status write failed and it is still new) is not counted twice.
func (r *rateLimiter) allow(ns, uid string, now time.Time) bool {
	if r == nil || r.limit <= 0 {
		return true
	}
	kept := r.admitted[ns][:0]
	for _, a := range r.admitted[ns] {
		if now.Sub(a.at) < r.window {
			kept = append(kept, a)
		}
	}
	r.admitted[ns] = kept
	for _, a := range kept {
		if uid != "" && a.uid == uid {
			return true
		}
	}
	if len(kept) >= r.limit {
		return false
	}
	r.admitted[ns] = append(kept, admission{uid: uid, at: now})
	return true
}
