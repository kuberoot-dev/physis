// Command physis-scheduler is the control-plane half of physis. It watches
// Problems, checks them against signed mandates, plans each one over the
// substrates whose agents are alive, using live grid forecasts, and writes the
// plan and its alternatives into the Problem's status. It never runs work and
// never binds anything to a node: an agent claims a planned problem. If a
// planned substrate stops sending heartbeats before claiming, the work goes
// back to Pending and is planned again (P7).
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"sync"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"

	v1 "github.com/kuberoot-dev/physis/api/v1alpha1"
	"github.com/kuberoot-dev/physis/pkg/device"
	"github.com/kuberoot-dev/physis/pkg/grid"
	"github.com/kuberoot-dev/physis/pkg/instances"
	"github.com/kuberoot-dev/physis/pkg/kube"
	"github.com/kuberoot-dev/physis/pkg/learn"
	"github.com/kuberoot-dev/physis/pkg/mandate"
	"github.com/kuberoot-dev/physis/pkg/plan"
)

var (
	kubeconfig  = flag.String("kubeconfig", "", "kubeconfig (empty: in-cluster)")
	learnPath   = flag.String("learn", "/etc/physis/learn.json", "calibration (learn.Store JSON)")
	gridDir     = flag.String("grid-dir", "", "use recorded grid CSVs instead of live data")
	staleAfter  = flag.Duration("stale-after", 30*time.Second, "an agent silent this long is considered gone")
	claimGrace  = flag.Duration("claim-grace", 2*time.Minute, "a plan not claimed this long after its start is planned again")
	interval    = flag.Duration("interval", 2*time.Second, "reconcile interval")
	systemNS    = flag.String("system-namespace", "physis-system", "namespace of the principals ConfigMap")
	interZoneJB = flag.Float64("inter-zone-j-per-byte", 4.77e-6, "energy of moving a byte between zones (ref:aslan2018, extrapolated to 2026)")
)

type scheduler struct {
	c     *kube.Client
	store *learn.Store
	mu    sync.Mutex
	grid  *grid.Provider
	seen  map[string]bool // problems whose results were folded into the store
}

func main() {
	flag.Parse()
	c, err := kube.New(*kubeconfig)
	if err != nil {
		log.Fatal(err)
	}
	f, err := os.Open(*learnPath)
	if err != nil {
		log.Fatal(err)
	}
	store, err := learn.Load(f)
	f.Close()
	if err != nil {
		log.Fatal(err)
	}
	s := &scheduler{c: c, store: store, seen: map[string]bool{}}
	if err := s.refreshGrid(); err != nil {
		log.Fatalf("grid: %v", err)
	}
	go func() {
		for range time.Tick(30 * time.Minute) {
			if err := s.refreshGrid(); err != nil {
				log.Printf("grid refresh: %v (keeping the previous data)", err)
			}
		}
	}()
	ctx := context.Background()
	for {
		if err := s.reconcile(ctx); err != nil {
			log.Printf("reconcile: %v", err)
		}
		time.Sleep(*interval)
	}
}

func (s *scheduler) refreshGrid() error {
	var p *grid.Provider
	var err error
	if *gridDir != "" {
		p, err = grid.LoadDir(*gridDir, []string{"DE", "FR", "PL", "GB"})
	} else {
		p, err = grid.FetchLive(&http.Client{Timeout: 2 * time.Minute}, grid.DefaultZones, time.Now().UTC())
	}
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.grid = p
	s.mu.Unlock()
	log.Printf("grid data loaded for %v", p.Names())
	return nil
}

func alive(sub *kube.Substrate, now time.Time) bool {
	return now.Sub(sub.Status.Heartbeat) < *staleAfter
}

func (s *scheduler) principals(ctx context.Context) map[string]string {
	cm, err := s.c.D.Resource(schema.GroupVersionResource{Version: "v1", Resource: "configmaps"}).Namespace(*systemNS).Get(ctx, "principals", metav1.GetOptions{})
	out := map[string]string{}
	if err != nil {
		return out
	}
	data, _ := cm.Object["data"].(map[string]any)
	for k, v := range data {
		if str, ok := v.(string); ok {
			out[k] = str
		}
	}
	return out
}

func (s *scheduler) mandateFor(ctx context.Context, p *kube.Problem) (*mandate.Mandate, error) {
	u, err := s.c.D.Resource(kube.Mandates).Namespace(p.Meta.Namespace).Get(ctx, p.Spec.Mandate, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("mandate %q: %w", p.Spec.Mandate, err)
	}
	m, err := kube.Decode[mandate.Mandate, struct{}](u)
	if err != nil {
		return nil, err
	}
	if err := m.Spec.Verify(s.principals(ctx)); err != nil {
		return nil, err
	}
	if m.Spec.Namespace != p.Meta.Namespace {
		return nil, fmt.Errorf("mandate covers namespace %q, not %q", m.Spec.Namespace, p.Meta.Namespace)
	}
	return &m.Spec, nil
}

func (s *scheduler) reconcile(ctx context.Context) error {
	now := time.Now().UTC()
	subs, err := s.c.ListSubstrates(ctx)
	if err != nil {
		return err
	}
	probs, err := s.c.ListProblems(ctx)
	if err != nil {
		return err
	}
	byName := map[string]*kube.Substrate{}
	for _, sub := range subs {
		byName[sub.Meta.Name] = sub
	}
	// Queue per substrate: work planned or running there ends no earlier
	// than its predicted finish.
	reserved := map[string][]plan.Interval{}
	spent := map[string]mandate.Spent{}
	day := now.Truncate(24 * time.Hour)
	for _, p := range probs {
		st := p.Status
		if st.Plan != nil && (st.Phase == v1.PhasePlanned || st.Phase == v1.PhaseRunning) && !p.Spec.DryRun {
			reserved[st.Plan.Substrate] = append(reserved[st.Plan.Substrate], plan.Interval{From: st.Plan.Start, To: st.Plan.Finish()})
		}
		if st.Phase == v1.PhaseSucceeded && st.Result != nil && st.ClaimedAt != nil && !st.ClaimedAt.Before(day) {
			sp := spent[p.Meta.Namespace]
			sp.Joules += st.Result.DeviceJoules
			sp.GCO2 += st.Result.GCO2
			spent[p.Meta.Namespace] = sp
		}
		s.learnFrom(p)
	}
	s.mu.Lock()
	g := s.grid
	s.mu.Unlock()
	var live []*plan.Substrate
	for _, sub := range subs {
		if !alive(sub, now) {
			continue
		}
		m := device.Builtin(device.Physics(sub.Spec.Physics))
		if m == nil {
			continue
		}
		live = append(live, &plan.Substrate{Name: sub.Meta.Name, Zone: sub.Spec.Zone, Operator: sub.Spec.Operator, Jurisdiction: sub.Spec.Jurisdiction, Model: m,
			Reserved: reserved[sub.Meta.Name], Slots: max(sub.Spec.Slots, 1)})
	}
	for _, p := range probs {
		switch p.Status.Phase {
		case "", v1.PhasePending:
			s.plan(ctx, p, live, g, now, spent[p.Meta.Namespace])
		case v1.PhaseAwaitingApproval:
			if p.Meta.Annotations[v1.AnnotationApproved] == "true" {
				p.Status.Phase = v1.PhasePlanned
				p.Status.Message = "approved"
				s.update(ctx, p)
			}
		case v1.PhasePlanned:
			if p.Spec.DryRun || p.Status.Plan == nil {
				continue
			}
			sub := byName[p.Status.Plan.Substrate]
			gone := sub == nil || !alive(sub, now)
			late := now.After(p.Status.Plan.Start.Add(*claimGrace))
			if gone || late {
				why := "not claimed in time"
				if gone {
					why = "substrate " + p.Status.Plan.Substrate + " stopped sending heartbeats"
				}
				p.Status.Phase = v1.PhasePending
				p.Status.Replans++
				p.Status.Message = "returned to the pool: " + why
				p.Status.Plan = nil
				s.update(ctx, p)
			}
		}
	}
	return nil
}

func (s *scheduler) learnFrom(p *kube.Problem) {
	st := p.Status
	if st.Phase != v1.PhaseSucceeded || st.Result == nil || st.Plan == nil || s.seen[string(p.Meta.UID)] {
		return
	}
	s.seen[string(p.Meta.UID)] = true
	shape, err := instances.Shape(p.Spec.Class, p.Spec.Instance)
	if err != nil {
		return
	}
	s.store.Observe(learn.Key{Class: string(shape.Class), Algo: st.Plan.Algorithm, Effort: st.Plan.Effort, Physics: string(st.Plan.Physics), SizeBucket: learn.Bucket(shape.Size())}, st.Result.Quality)
	s.store.Measure(st.Plan.Substrate, "joules", st.Result.DeviceJoules, st.Plan.ModelJoules)
}

func (s *scheduler) update(ctx context.Context, p *kube.Problem) {
	if _, err := s.c.UpdateProblemStatus(ctx, p); err != nil {
		log.Printf("%s/%s: %v", p.Meta.Namespace, p.Meta.Name, err)
	}
}

func (s *scheduler) reject(ctx context.Context, p *kube.Problem, msg string, reasons []string) {
	p.Status.Phase = v1.PhaseRejected
	p.Status.Message = msg
	p.Status.Reasons = reasons
	s.update(ctx, p)
	log.Printf("%s/%s rejected: %s", p.Meta.Namespace, p.Meta.Name, msg)
}

func (s *scheduler) plan(ctx context.Context, p *kube.Problem, live []*plan.Substrate, g *grid.Provider, now time.Time, spent mandate.Spent) {
	var m *mandate.Mandate
	if p.Spec.Mandate != "" {
		var err error
		if m, err = s.mandateFor(ctx, p); err != nil {
			s.reject(ctx, p, "no valid mandate: "+err.Error(), nil)
			return
		}
	}
	shape, err := instances.Shape(p.Spec.Class, p.Spec.Instance)
	if err != nil {
		s.reject(ctx, p, err.Error(), nil)
		return
	}
	deadline := p.Meta.CreationTimestamp.Time.Add(time.Duration(p.Spec.DeadlineSeconds) * time.Second)
	if p.Spec.Deadline != nil {
		deadline = *p.Spec.Deadline
	}
	zones := p.Spec.Zones
	if m != nil && len(m.Zones) > 0 {
		zones = intersect(zones, m.Zones)
		if len(zones) == 0 {
			s.reject(ctx, p, "the problem's zones and the mandate's zones do not overlap", nil)
			return
		}
	}
	req := &plan.Request{ID: p.Meta.Name, Shape: shape, Quality: p.Spec.Quality, Confidence: p.Spec.Confidence, Deadline: deadline,
		Deferrable: p.Spec.Deferrable, Policy: plan.Policy(p.Spec.Policy), Budget: p.Spec.Budget, Zones: zones,
		Jurisdictions: p.Spec.Jurisdictions, DataZone: p.Spec.DataZone, DataBytes: p.Spec.DataBytes}
	if len(p.Spec.Physics) > 0 {
		req.Pin = &plan.PinAlgorithm{}
		for _, x := range p.Spec.Physics {
			req.Pin.Physics = append(req.Pin.Physics, device.Physics(x))
		}
	}
	env := &plan.Env{Substrates: live, Grid: g, Learn: s.store, Now: now, PUE: 1.2, InterZoneJPerByte: *interZoneJB, IntraZoneJPerByte: *interZoneJB / 10}
	d, err := plan.Plan(env, req)
	if err != nil {
		s.reject(ctx, p, err.Error(), nil)
		return
	}
	if d.Chosen == nil {
		s.reject(ctx, p, fmt.Sprintf("no way to reach quality %.3f by %s within the budget on the %d live substrates", p.Spec.Quality, deadline.Format(time.RFC3339), len(live)), d.Reasons)
		return
	}
	t := now
	p.Status.Plan, p.Status.Pareto, p.Status.Candidates, p.Status.Reasons, p.Status.PlannedAt = d.Chosen, d.Pareto, d.Count, d.Reasons, &t
	p.Status.FeasiblePhysics = nil
	for _, ph := range d.Physics {
		p.Status.FeasiblePhysics = append(p.Status.FeasiblePhysics, string(ph))
	}
	p.Status.Phase = v1.PhasePlanned
	p.Status.Message = ""
	if m != nil {
		v := m.Admit(now, d.Chosen.Zone, d.Chosen.Joules, d.Chosen.EUR, d.Chosen.GCO2, spent)
		if !v.Allowed {
			s.reject(ctx, p, "mandate: "+v.Reason, nil)
			return
		}
		if v.NeedsApproval && p.Meta.Annotations[v1.AnnotationApproved] != "true" {
			p.Status.Phase = v1.PhaseAwaitingApproval
			p.Status.Message = v.Reason
		}
	}
	if p.Spec.DryRun {
		p.Status.Message = "dry run: plan only, nothing will run"
	}
	s.update(ctx, p)
	log.Printf("%s/%s planned on %s (%s/%s) at %s: %.3g J, %.3g gCO2", p.Meta.Namespace, p.Meta.Name, d.Chosen.Substrate, d.Chosen.Algorithm, d.Chosen.Effort, d.Chosen.Start.Format(time.RFC3339), d.Chosen.Joules, d.Chosen.GCO2)
}

func intersect(a, b []string) []string {
	if len(a) == 0 {
		return b
	}
	var out []string
	for _, x := range a {
		for _, y := range b {
			if x == y {
				out = append(out, x)
			}
		}
	}
	return out
}
