package main

// Hierarchical planning. A single scheduler plans everything (role
// "single", the default). With roles, planning splits the way grid control
// does: the global level chooses the zone and the earliest start from grid
// forecasts and prices; a site scheduler per zone chooses the substrate and
// the moment inside it, where it knows the queues; the agent decides locally
// how to run (see physis-agent). The levels announce themselves with Leases,
// and a lower level keeps working when the one above is gone: a site plans
// the work that may run in its zone on its own while the centre is silent,
// and the centre takes delegated work back from a site that went silent.

import (
	"context"
	"flag"
	"log"
	"strings"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	v1 "github.com/kuberoot-dev/physis/api/v1alpha1"
	"github.com/kuberoot-dev/physis/pkg/kube"
)

var (
	role       = flag.String("role", "single", "single, global or site")
	siteZone   = flag.String("site-zone", "", "the zone of a site scheduler (role site)")
	leaseStale = flag.Duration("lease-stale", 15*time.Second, "a level whose Lease is older than this is considered gone")
)

var leases = schema.GroupVersionResource{Group: "coordination.k8s.io", Version: "v1", Resource: "leases"}

// leaseName is a valid object name: zones are upper-case, names are not.
func leaseName(level string) string { return "physis-" + strings.ToLower(level) }

func myLevel() string {
	if *role == "site" {
		return "site-" + *siteZone
	}
	return *role
}

// renewLease announces this level.
func (s *scheduler) renewLease(ctx context.Context) {
	if *role == "single" {
		return
	}
	ri := s.c.D.Resource(leases).Namespace(*systemNS)
	now := metav1.NewMicroTime(time.Now())
	name := leaseName(myLevel())
	cur, err := ri.Get(ctx, name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		u := &unstructured.Unstructured{Object: map[string]any{
			"apiVersion": "coordination.k8s.io/v1", "kind": "Lease",
			"metadata": map[string]any{"name": name, "namespace": *systemNS},
			"spec":     map[string]any{"holderIdentity": name, "renewTime": now.Format(time.RFC3339Nano), "leaseDurationSeconds": int64(leaseStale.Seconds())},
		}}
		if _, err := ri.Create(ctx, u, metav1.CreateOptions{}); err != nil {
			log.Printf("lease %s: %v", name, err)
		}
		return
	}
	if err != nil {
		log.Printf("lease %s: %v", name, err)
		return
	}
	_ = unstructured.SetNestedField(cur.Object, now.Format(time.RFC3339Nano), "spec", "renewTime")
	if _, err := ri.Update(ctx, cur, metav1.UpdateOptions{}); err != nil {
		log.Printf("lease %s: %v", name, err)
	}
}

// fresh says whether a level announced itself recently.
func (s *scheduler) fresh(ctx context.Context, level string) bool {
	u, err := s.c.D.Resource(leases).Namespace(*systemNS).Get(ctx, leaseName(level), metav1.GetOptions{})
	if err != nil {
		return false
	}
	ts, _, _ := unstructured.NestedString(u.Object, "spec", "renewTime")
	t, err := time.Parse(time.RFC3339Nano, ts)
	return err == nil && time.Since(t) < *leaseStale
}

// mine says whether this scheduler should plan or replan a problem now.
func (s *scheduler) mine(ctx context.Context, p *kube.Problem) bool {
	st := p.Status
	switch *role {
	case "single":
		return st.Phase != v1.PhaseZonePlanned
	case "global":
		switch {
		case st.Phase == v1.PhaseZonePlanned:
			// Delegated work comes back only if its site went silent.
			return !s.fresh(ctx, "site-"+st.Zone)
		case len(st.PlannedBy) > 5 && st.PlannedBy[:5] == "site:":
			return !s.fresh(ctx, "site-"+st.PlannedBy[5:])
		}
		return true
	case "site":
		if st.Phase == v1.PhaseZonePlanned {
			return st.Zone == *siteZone
		}
		if st.PlannedBy == "site:"+*siteZone {
			return true
		}
		// Without a centre, a site plans what may run in its zone.
		if s.fresh(ctx, "global") {
			return false
		}
		return allows(p.Spec.Zones, *siteZone)
	}
	return false
}

func allows(zones []string, z string) bool {
	if len(zones) == 0 {
		return true
	}
	for _, x := range zones {
		if x == z {
			return true
		}
	}
	return false
}

// delegate turns a full plan into a zone assignment when a site serves the
// chosen zone; it reports whether it did.
func (s *scheduler) delegate(ctx context.Context, p *kube.Problem) bool {
	if *role != "global" || p.Spec.DryRun || p.Status.Plan == nil || p.Status.Phase != v1.PhasePlanned {
		return false
	}
	z := p.Status.Plan.Zone
	if !s.fresh(ctx, "site-"+z) {
		return false
	}
	start := p.Status.Plan.Start
	p.Status.Phase, p.Status.Zone, p.Status.NotBefore, p.Status.PlannedBy = v1.PhaseZonePlanned, z, &start, "global"
	p.Status.Plan = nil
	p.Status.Message = "zone " + z + " chosen by the global level; the site chooses the substrate"
	return true
}

// plannedBy names this level in a plan it makes.
func plannedBy() string {
	switch *role {
	case "site":
		return "site:" + *siteZone
	case "global":
		return "global"
	}
	return ""
}
