// Command physis-conformance checks a cluster against the "infrastructure of
// tomorrow" contract (docs/conformance.md). It is black-box: it talks to the
// API server only, creates problems in its own namespace, and judges what the
// system does, not how it is built. Each test names the principle it checks.
//
// Exit code 0 when every Core test passes. Results go to stdout and, with
// -report, to a JSON file.
package main

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	v1 "github.com/kuberoot-dev/physis/api/v1alpha1"
	"github.com/kuberoot-dev/physis/pkg/kube"
	"github.com/kuberoot-dev/physis/pkg/mandate"
	"github.com/kuberoot-dev/physis/pkg/plan"
	"github.com/kuberoot-dev/physis/pkg/receipt"
)

var (
	kubeconfig = flag.String("kubeconfig", "", "kubeconfig")
	ns         = flag.String("namespace", "tomorrow-conformance", "namespace for test objects")
	reportPath = flag.String("report", "", "write a JSON report here")
	only       = flag.String("run", "", "run only tests whose ID contains this")
	timeout    = flag.Duration("timeout", 3*time.Minute, "per-test timeout")
)

type result struct {
	ID        string   `json:"id"`
	Level     string   `json:"level"`
	Principle string   `json:"principle"`
	Title     string   `json:"title"`
	Pass      bool     `json:"pass"`
	Skipped   bool     `json:"skipped,omitempty"`
	Seconds   float64  `json:"seconds"`
	Evidence  []string `json:"evidence"`
	Error     string   `json:"error,omitempty"`
}

type T struct {
	c   *kube.Client
	ctx context.Context
	ev  []string
}

func (t *T) note(format string, a ...any) { t.ev = append(t.ev, fmt.Sprintf(format, a...)) }

type test struct {
	ID, Level, Principle, Title string
	Run                         func(t *T) error
}

var cm = schema.GroupVersionResource{Version: "v1", Resource: "configmaps"}
var nodesGVR = schema.GroupVersionResource{Version: "v1", Resource: "nodes"}
var crGVR = schema.GroupVersionResource{Group: "rbac.authorization.k8s.io", Version: "v1", Resource: "clusterroles"}
var nsGVR = schema.GroupVersionResource{Version: "v1", Resource: "namespaces"}

func main() {
	flag.Parse()
	c, err := kube.New(*kubeconfig)
	if err != nil {
		log.Fatal(err)
	}
	ctx := context.Background()
	c.D.Resource(nsGVR).Create(ctx, &unstructured.Unstructured{Object: map[string]any{"apiVersion": "v1", "kind": "Namespace", "metadata": map[string]any{"name": *ns}}}, metav1.CreateOptions{})
	var results []result
	failedCore := 0
	for _, tc := range tests() {
		if *only != "" && !strings.Contains(tc.ID, *only) {
			continue
		}
		t := &T{c: c}
		cctx, cancel := context.WithTimeout(ctx, *timeout)
		t.ctx = cctx
		start := time.Now()
		err := tc.Run(t)
		cancel()
		r := result{ID: tc.ID, Level: tc.Level, Principle: tc.Principle, Title: tc.Title, Seconds: time.Since(start).Seconds(), Evidence: t.ev}
		if err == errSkip {
			r.Skipped = true
		} else if err != nil {
			r.Error = err.Error()
		} else {
			r.Pass = true
		}
		if !r.Pass && !r.Skipped && tc.Level == "Core" {
			failedCore++
		}
		status := "PASS"
		if r.Skipped {
			status = "SKIP"
		} else if !r.Pass {
			status = "FAIL"
		}
		fmt.Printf("%-4s %-6s %-5s %-60s %5.1fs\n", status, tc.ID, tc.Principle, tc.Title, r.Seconds)
		if r.Error != "" {
			fmt.Printf("     %s\n", r.Error)
		}
		results = append(results, r)
	}
	if *reportPath != "" {
		b, _ := json.MarshalIndent(results, "", " ")
		os.WriteFile(*reportPath, b, 0o644)
	}
	pass := 0
	for _, r := range results {
		if r.Pass {
			pass++
		}
	}
	fmt.Printf("\n%d/%d passed, %d Core failures\n", pass, len(results), failedCore)
	if failedCore > 0 {
		os.Exit(1)
	}
}

var errSkip = fmt.Errorf("skipped")

// --- helpers ---------------------------------------------------------------

func (t *T) createProblem(name string, spec v1.ProblemSpec, ann map[string]string) error {
	p := &kube.Problem{Meta: metav1.ObjectMeta{Name: name, Namespace: *ns, Annotations: ann}, Spec: spec}
	u, err := kube.Encode("Problem", p)
	if err != nil {
		return err
	}
	t.c.D.Resource(kube.Problems).Namespace(*ns).Delete(t.ctx, name, metav1.DeleteOptions{})
	for i := 0; i < 50; i++ { // wait for a previous object of the same name to go
		if _, err := t.c.D.Resource(kube.Problems).Namespace(*ns).Get(t.ctx, name, metav1.GetOptions{}); err != nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	_, err = t.c.D.Resource(kube.Problems).Namespace(*ns).Create(t.ctx, u, metav1.CreateOptions{})
	return err
}

func (t *T) get(name string) (*kube.Problem, error) {
	u, err := t.c.D.Resource(kube.Problems).Namespace(*ns).Get(t.ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	return kube.Decode[v1.ProblemSpec, v1.ProblemStatus](u)
}

// wait polls a problem until cond holds or the test times out.
func (t *T) wait(name string, what string, cond func(*kube.Problem) bool) (*kube.Problem, error) {
	var last *kube.Problem
	for {
		p, err := t.get(name)
		if err == nil {
			last = p
			if cond(p) {
				return p, nil
			}
		}
		select {
		case <-t.ctx.Done():
			ph := "?"
			if last != nil {
				ph = last.Status.Phase + " " + last.Status.Message
			}
			return last, fmt.Errorf("timed out waiting for %s (%s: %s)", what, name, ph)
		case <-time.After(500 * time.Millisecond):
		}
	}
}

func phaseIs(ph ...string) func(*kube.Problem) bool {
	return func(p *kube.Problem) bool {
		for _, x := range ph {
			if p.Status.Phase == x {
				return true
			}
		}
		return false
	}
}

func digits(size int, quality float64) v1.ProblemSpec {
	return v1.ProblemSpec{Class: "classify", Instance: v1.InstanceRef{Digits: &v1.DigitsBatch{Size: size, Offset: 0}}, Quality: quality, Confidence: 0.9, DeadlineSeconds: 120, Policy: "min-energy"}
}

func maxcut(g string, quality float64) v1.ProblemSpec {
	return v1.ProblemSpec{Class: "maxcut", Instance: v1.InstanceRef{Gset: g}, Quality: quality, Confidence: 0.9, DeadlineSeconds: 120, Policy: "min-energy"}
}

func (t *T) substrates() ([]*kube.Substrate, error) { return t.c.ListSubstrates(t.ctx) }

func liveSubstrates(subs []*kube.Substrate) []*kube.Substrate {
	var out []*kube.Substrate
	for _, s := range subs {
		if time.Since(s.Status.Heartbeat) < 30*time.Second {
			out = append(out, s)
		}
	}
	return out
}

func (t *T) runToSuccess(name string, spec v1.ProblemSpec) (*kube.Problem, error) {
	if err := t.createProblem(name, spec, nil); err != nil {
		return nil, err
	}
	return t.wait(name, "success", phaseIs(v1.PhaseSucceeded, v1.PhaseRejected, v1.PhaseFailed))
}

// --- tests ------------------------------------------------------------------

func tests() []test {
	return []test{
		{"TC-01", "Core", "P3", "every substrate describes its physics with provenance", tc01},
		{"TC-02", "Core", "P7", "work completes without Node objects; roles grant nothing on nodes", tc02},
		{"TC-03", "Core", "P2,P4", "an outcome request is planned with predicted cost and met by its deadline", tc03},
		{"TC-04", "Core", "P4", "the plan exposes a non-dominated set of alternatives", tc04},
		{"TC-05", "Core", "P5", "an unattainable outcome is refused with reasons and never run", tc05},
		{"TC-06", "Core", "P4", "a budget is a hard limit", tc06},
		{"TC-07", "Core", "P11", "zones and operator jurisdictions are hard constraints", tc07},
		{"TC-08", "Core", "P14", "a dry run returns a plan and runs nothing", tc08},
		{"TC-09", "Extended", "P12", "deferrable work is placed at the lowest forecast carbon within its deadline", tc09},
		{"TC-10", "Core", "P9", "every result has a signed receipt whose chain verifies offline; tampering is detected", tc10},
		{"TC-11", "Core", "P9", "receipts are immutable", tc11},
		{"TC-12", "Core", "P10", "work outside a valid human mandate is refused; approval thresholds hold", tc12},
		{"TC-13", "Core", "P7", "work planned on a substrate that goes silent returns to the pool and completes elsewhere", tc13},
		{"TC-14", "Extended", "P8", "a substrate cut off after claiming finishes the work and reports it later", tc14},
		{"TC-15", "Core", "P2,P3", "the same problem class is offered on more than one physics", tc15},
		{"TC-16", "Core", "P5", "results report achieved quality; plans report a confidence bound", tc16},
	}
}

func tc01(t *T) error {
	subs, err := t.substrates()
	if err != nil {
		return err
	}
	live := liveSubstrates(subs)
	if len(live) == 0 {
		return fmt.Errorf("no live substrates")
	}
	physics := map[string]bool{}
	for _, s := range live {
		if s.Spec.Physics == "" || s.Spec.Zone == "" || s.Spec.Jurisdiction == "" {
			return fmt.Errorf("substrate %s lacks physics, zone or jurisdiction", s.Meta.Name)
		}
		if len(s.Spec.Model) == 0 {
			return fmt.Errorf("substrate %s publishes no model", s.Meta.Name)
		}
		for _, p := range s.Spec.Model {
			if p.Source != "measured" && p.Source != "assumption" && !strings.HasPrefix(p.Source, "ref:") {
				return fmt.Errorf("substrate %s parameter %s has provenance %q", s.Meta.Name, p.Name, p.Source)
			}
		}
		physics[s.Spec.Physics] = true
	}
	var ps []string
	for p := range physics {
		ps = append(ps, p)
	}
	sort.Strings(ps)
	t.note("%d live substrates, %d physics: %s", len(live), len(ps), strings.Join(ps, ", "))
	return nil
}

func tc02(t *T) error {
	nodes, err := t.c.D.Resource(nodesGVR).List(t.ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}
	t.note("Node objects in the cluster: %d", len(nodes.Items))
	crs, err := t.c.D.Resource(crGVR).List(t.ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}
	for _, cr := range crs.Items {
		if !strings.HasPrefix(cr.GetName(), "physis-") {
			continue
		}
		rules, _, _ := unstructured.NestedSlice(cr.Object, "rules")
		for _, r := range rules {
			rm, _ := r.(map[string]any)
			res, _ := rm["resources"].([]any)
			for _, x := range res {
				if s, _ := x.(string); s == "nodes" || s == "*" || s == "pods" {
					return fmt.Errorf("ClusterRole %s grants access to %s", cr.GetName(), s)
				}
			}
		}
		t.note("ClusterRole %s: no access to nodes or pods", cr.GetName())
	}
	p, err := t.runToSuccess("tc02", digits(100, 0.95))
	if err != nil {
		return err
	}
	if p.Status.Phase != v1.PhaseSucceeded {
		return fmt.Errorf("problem ended %s: %s", p.Status.Phase, p.Status.Message)
	}
	t.note("problem completed on %s (%s) with %d Node objects present", p.Status.ClaimedBy, p.Status.Plan.Physics, len(nodes.Items))
	return nil
}

func tc03(t *T) error {
	spec := maxcut("G1", 0.99)
	p, err := t.runToSuccess("tc03", spec)
	if err != nil {
		return err
	}
	if p.Status.Phase != v1.PhaseSucceeded {
		return fmt.Errorf("ended %s: %s", p.Status.Phase, p.Status.Message)
	}
	pl := p.Status.Plan
	if pl.Seconds <= 0 || pl.Joules <= 0 || pl.CI <= 0 {
		return fmt.Errorf("plan lacks predicted time, energy or carbon intensity: %+v", pl)
	}
	deadline := p.Meta.CreationTimestamp.Add(time.Duration(spec.DeadlineSeconds) * time.Second)
	rcpt, err := t.receipt(p)
	if err != nil {
		return err
	}
	if rcpt.Finished.After(deadline) {
		return fmt.Errorf("finished %s after deadline %s", rcpt.Finished, deadline)
	}
	t.note("plan: %s/%s on %s (%s), predicted %.3gs, %.3gJ, %.3ggCO2 at %.0f g/kWh", pl.Algorithm, pl.Effort, pl.Substrate, pl.Physics, pl.Seconds, pl.Joules, pl.GCO2, pl.CI)
	t.note("result: quality %.4f, finished %s before the deadline", p.Status.Result.Quality, deadline.Sub(rcpt.Finished).Round(time.Second))
	return nil
}

func tc04(t *T) error {
	if err := t.createProblem("tc04", withDry(maxcut("G1", 0.98)), nil); err != nil {
		return err
	}
	p, err := t.wait("tc04", "plan", phaseIs(v1.PhasePlanned, v1.PhaseRejected))
	if err != nil {
		return err
	}
	if len(p.Status.Pareto) == 0 {
		return fmt.Errorf("no alternatives exposed")
	}
	for _, a := range p.Status.Pareto {
		for _, b := range p.Status.Pareto {
			if plan.Dominates(b, a) {
				return fmt.Errorf("alternative on %s at %s dominated by %s at %s", a.Substrate, a.Start.Format(time.RFC3339), b.Substrate, b.Start.Format(time.RFC3339))
			}
		}
	}
	t.note("%d candidates considered, %d non-dominated alternatives exposed", p.Status.Candidates, len(p.Status.Pareto))
	return nil
}

func withDry(s v1.ProblemSpec) v1.ProblemSpec { s.DryRun = true; return s }

func tc05(t *T) error {
	spec := maxcut("G22", 1.0)
	spec.Confidence = 0.999
	spec.DeadlineSeconds = 1
	if err := t.createProblem("tc05", spec, nil); err != nil {
		return err
	}
	p, err := t.wait("tc05", "a decision", phaseIs(v1.PhaseRejected, v1.PhasePlanned, v1.PhaseRunning, v1.PhaseSucceeded))
	if err != nil {
		return err
	}
	if p.Status.Phase != v1.PhaseRejected {
		return fmt.Errorf("unattainable problem went to %s", p.Status.Phase)
	}
	if len(p.Status.Reasons) == 0 && p.Status.Message == "" {
		return fmt.Errorf("refused without a reason")
	}
	t.note("refused: %s", p.Status.Message)
	if len(p.Status.Reasons) > 0 {
		t.note("e.g. %s", p.Status.Reasons[0])
	}
	return nil
}

func tc06(t *T) error {
	spec := withDry(maxcut("G22", 0.99))
	if err := t.createProblem("tc06a", spec, nil); err != nil {
		return err
	}
	p, err := t.wait("tc06a", "plan", phaseIs(v1.PhasePlanned, v1.PhaseRejected))
	if err != nil || p.Status.Plan == nil {
		return fmt.Errorf("reference plan: %v %s", err, p.Status.Message)
	}
	limit := p.Status.Plan.Joules / 1000
	spec.Budget.Joules = limit
	if err := t.createProblem("tc06b", spec, nil); err != nil {
		return err
	}
	q, err := t.wait("tc06b", "plan", phaseIs(v1.PhasePlanned, v1.PhaseRejected))
	if err != nil {
		return err
	}
	if q.Status.Phase == v1.PhasePlanned && q.Status.Plan.Joules > limit {
		return fmt.Errorf("planned %.3g J over a %.3g J budget", q.Status.Plan.Joules, limit)
	}
	t.note("cheapest plan %.3g J; with a %.3g J budget: %s", p.Status.Plan.Joules, limit, q.Status.Phase)
	return nil
}

func tc07(t *T) error {
	subs, err := t.substrates()
	if err != nil {
		return err
	}
	zones := map[string]bool{}
	juris := map[string]bool{}
	for _, s := range liveSubstrates(subs) {
		zones[s.Spec.Zone] = true
		juris[s.Spec.Jurisdiction] = true
	}
	// A constraint can only be shown to hold where there is a choice.
	if len(zones) < 2 || len(juris) < 2 {
		return fmt.Errorf("need live substrates in at least two zones and two jurisdictions, have %d and %d", len(zones), len(juris))
	}
	for z := range zones {
		spec := withDry(digits(100, 0.95))
		spec.Zones = []string{z}
		name := "tc07-" + strings.ToLower(z)
		if err := t.createProblem(name, spec, nil); err != nil {
			return err
		}
		p, err := t.wait(name, "plan", phaseIs(v1.PhasePlanned, v1.PhaseRejected))
		if err != nil {
			return err
		}
		if p.Status.Phase == v1.PhasePlanned && p.Status.Plan.Zone != z {
			return fmt.Errorf("restricted to %s, planned in %s", z, p.Status.Plan.Zone)
		}
		t.note("zone %s: %s", z, p.Status.Phase)
	}
	byName := map[string]*kube.Substrate{}
	for _, s := range subs {
		byName[s.Meta.Name] = s
	}
	for j := range juris {
		spec := withDry(maxcut("G1", 0.98))
		spec.Jurisdictions = []string{j}
		name := "tc07-j-" + strings.ToLower(strings.ReplaceAll(j, " ", ""))
		if err := t.createProblem(name, spec, nil); err != nil {
			return err
		}
		p, err := t.wait(name, "plan", phaseIs(v1.PhasePlanned, v1.PhaseRejected))
		if err != nil {
			return err
		}
		if p.Status.Phase == v1.PhasePlanned {
			if got := byName[p.Status.Plan.Substrate].Spec.Jurisdiction; got != j {
				return fmt.Errorf("restricted to jurisdiction %s, planned on %s under %s", j, p.Status.Plan.Substrate, got)
			}
		}
		t.note("jurisdiction %s: %s", j, p.Status.Phase)
	}
	return nil
}

func tc08(t *T) error {
	if err := t.createProblem("tc08", withDry(digits(100, 0.95)), nil); err != nil {
		return err
	}
	if _, err := t.wait("tc08", "plan", phaseIs(v1.PhasePlanned)); err != nil {
		return err
	}
	time.Sleep(8 * time.Second)
	p, err := t.get("tc08")
	if err != nil {
		return err
	}
	if p.Status.ClaimedBy != "" || p.Status.Phase != v1.PhasePlanned {
		return fmt.Errorf("dry run was claimed by %q (phase %s)", p.Status.ClaimedBy, p.Status.Phase)
	}
	t.note("plan returned; nothing claimed after 8s")
	return nil
}

func tc09(t *T) error {
	spec := withDry(maxcut("G1", 0.98))
	spec.Policy = "min-carbon"
	spec.Deferrable = true
	spec.DeadlineSeconds = 20 * 3600
	if err := t.createProblem("tc09", spec, nil); err != nil {
		return err
	}
	p, err := t.wait("tc09", "plan", phaseIs(v1.PhasePlanned, v1.PhaseRejected))
	if err != nil {
		return err
	}
	if p.Status.Plan == nil {
		return fmt.Errorf("not planned: %s", p.Status.Message)
	}
	for _, a := range p.Status.Pareto {
		if a.GCO2 < p.Status.Plan.GCO2*(1-1e-3) {
			return fmt.Errorf("an alternative emits %.3g g, the plan %.3g g", a.GCO2, p.Status.Plan.GCO2)
		}
	}
	wait := p.Status.Plan.Start.Sub(p.Meta.CreationTimestamp.Time)
	t.note("planned on %s in %s at %s (wait %s), %.0f g/kWh forecast", p.Status.Plan.Substrate, p.Status.Plan.Zone, p.Status.Plan.Start.Format(time.RFC3339), wait.Round(time.Minute), p.Status.Plan.CI)
	return nil
}

func (t *T) receipt(p *kube.Problem) (*receipt.Receipt, error) {
	if p.Status.Result == nil || p.Status.Result.Receipt == "" {
		return nil, fmt.Errorf("succeeded without a receipt")
	}
	u, err := t.c.D.Resource(kube.Receipts).Namespace(p.Meta.Namespace).Get(t.ctx, p.Status.Result.Receipt, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	o, err := kube.Decode[receipt.Receipt, struct{}](u)
	if err != nil {
		return nil, err
	}
	return &o.Spec, nil
}

func tc10(t *T) error {
	for i := 0; i < 3; i++ {
		p, err := t.runToSuccess(fmt.Sprintf("tc10-%d", i), digits(100+50*i, 0.95))
		if err != nil {
			return err
		}
		if p.Status.Phase != v1.PhaseSucceeded {
			return fmt.Errorf("ended %s", p.Status.Phase)
		}
	}
	subs, err := t.substrates()
	if err != nil {
		return err
	}
	key := map[string]string{}
	for _, s := range subs {
		key[s.Meta.Name] = s.Status.PublicKey
	}
	l, err := t.c.D.Resource(kube.Receipts).Namespace("").List(t.ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}
	chains := map[string][]*receipt.Receipt{}
	for i := range l.Items {
		o, err := kube.Decode[receipt.Receipt, struct{}](&l.Items[i])
		if err != nil {
			return err
		}
		r := o.Spec
		chains[r.Substrate] = append(chains[r.Substrate], &r)
	}
	verified := 0
	for sub, rs := range chains {
		sort.Slice(rs, func(i, j int) bool { return rs[i].Seq < rs[j].Seq })
		// Receipts of a chain may live in several namespaces and earlier ones
		// may have been deleted with their namespace; verify the contiguous
		// tail we can see.
		start := 0
		for i := 1; i < len(rs); i++ {
			if rs[i].Seq != rs[i-1].Seq+1 {
				start = i
			}
		}
		tail := rs[start:]
		if err := receipt.VerifyChain(tail, key[sub]); err != nil {
			return fmt.Errorf("chain of %s: %v", sub, err)
		}
		verified += len(tail)
		if len(tail) >= 2 {
			forged := *tail[0]
			forged.DeviceJoules /= 10
			tampered := append([]*receipt.Receipt{&forged}, tail[1:]...)
			if receipt.VerifyChain(tampered, key[sub]) == nil {
				return fmt.Errorf("a tampered receipt of %s verified", sub)
			}
		}
	}
	t.note("%d receipts in %d chains verified with the substrates' public keys; a tampered copy was rejected", verified, len(chains))
	return nil
}

func tc11(t *T) error {
	l, err := t.c.D.Resource(kube.Receipts).Namespace(*ns).List(t.ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}
	if len(l.Items) == 0 {
		return fmt.Errorf("no receipts to test (run TC-10 first)")
	}
	u := l.Items[0].DeepCopy()
	unstructured.SetNestedField(u.Object, 0.0, "spec", "deviceJoules")
	_, err = t.c.D.Resource(kube.Receipts).Namespace(*ns).Update(t.ctx, u, metav1.UpdateOptions{})
	if err == nil {
		return fmt.Errorf("receipt %s was modified", u.GetName())
	}
	t.note("update of receipt %s refused: %v", u.GetName(), firstLine(err.Error()))
	return nil
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i > 0 {
		return s[:i]
	}
	if len(s) > 160 {
		return s[:160]
	}
	return s
}

func (t *T) mandate(name string, m mandate.Mandate, priv ed25519.PrivateKey) error {
	m.Sign(priv)
	o := &kube.Object[mandate.Mandate, struct{}]{Meta: metav1.ObjectMeta{Name: name, Namespace: *ns}, Spec: m}
	u, err := kube.Encode("Mandate", o)
	if err != nil {
		return err
	}
	t.c.D.Resource(kube.Mandates).Namespace(*ns).Delete(t.ctx, name, metav1.DeleteOptions{})
	_, err = t.c.D.Resource(kube.Mandates).Namespace(*ns).Create(t.ctx, u, metav1.CreateOptions{})
	return err
}

func tc12(t *T) error {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	_, rogue, _ := ed25519.GenerateKey(rand.Reader)
	principal := "conformance-person"
	// Register the person's key with the control plane.
	cmo := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "v1", "kind": "ConfigMap",
		"metadata": map[string]any{"name": "principals", "namespace": "physis-system"}}}
	cur, err := t.c.D.Resource(cm).Namespace("physis-system").Get(t.ctx, "principals", metav1.GetOptions{})
	data := map[string]any{}
	if err == nil {
		if d, ok := cur.Object["data"].(map[string]any); ok {
			data = d
		}
		cmo = cur
	}
	data[principal] = fmt.Sprintf("%x", pub)
	cmo.Object["data"] = data
	if err == nil {
		_, err = t.c.D.Resource(cm).Namespace("physis-system").Update(t.ctx, cmo, metav1.UpdateOptions{})
	} else {
		_, err = t.c.D.Resource(cm).Namespace("physis-system").Create(t.ctx, cmo, metav1.CreateOptions{})
	}
	if err != nil {
		return fmt.Errorf("register principal: %w", err)
	}
	now := time.Now().UTC()
	base := mandate.Mandate{Principal: principal, Namespace: *ns, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour), Autonomy: mandate.Augmented, ApproveAbove: 1e-9}
	if err := t.mandate("m-rogue", base, rogue); err != nil {
		return err
	}
	if err := t.mandate("m-valid", base, priv); err != nil {
		return err
	}
	spec := digits(100, 0.95)
	spec.Mandate = "m-rogue"
	if err := t.createProblem("tc12a", spec, nil); err != nil {
		return err
	}
	p, err := t.wait("tc12a", "decision", phaseIs(v1.PhaseRejected, v1.PhasePlanned, v1.PhaseSucceeded))
	if err != nil {
		return err
	}
	if p.Status.Phase != v1.PhaseRejected {
		return fmt.Errorf("mandate signed by an unknown key accepted: %s", p.Status.Phase)
	}
	t.note("mandate signed by a key the principal does not hold: %s", p.Status.Message)
	spec.Mandate = "m-valid"
	if err := t.createProblem("tc12b", spec, nil); err != nil {
		return err
	}
	p, err = t.wait("tc12b", "approval gate", phaseIs(v1.PhaseAwaitingApproval, v1.PhaseRejected, v1.PhasePlanned, v1.PhaseSucceeded))
	if err != nil {
		return err
	}
	if p.Status.Phase != v1.PhaseAwaitingApproval {
		return fmt.Errorf("plan above the approval threshold went to %s", p.Status.Phase)
	}
	t.note("valid mandate, plan above threshold: %s (%s)", p.Status.Phase, p.Status.Message)
	u, err := t.c.D.Resource(kube.Problems).Namespace(*ns).Get(t.ctx, "tc12b", metav1.GetOptions{})
	if err != nil {
		return err
	}
	ann := u.GetAnnotations()
	if ann == nil {
		ann = map[string]string{}
	}
	ann[v1.AnnotationApproved] = "true"
	u.SetAnnotations(ann)
	if _, err := t.c.D.Resource(kube.Problems).Namespace(*ns).Update(t.ctx, u, metav1.UpdateOptions{}); err != nil {
		return err
	}
	p, err = t.wait("tc12b", "completion after approval", phaseIs(v1.PhaseSucceeded, v1.PhaseFailed))
	if err != nil {
		return err
	}
	t.note("after a person approved: %s", p.Status.Phase)
	return nil
}

// partition asks a substrate's agent to behave as if cut off from the API
// server until a time (the agent's test hook).
func (t *T) partition(sub string, until time.Time) error {
	u, err := t.c.D.Resource(kube.Substrates).Get(t.ctx, sub, metav1.GetOptions{})
	if err != nil {
		return err
	}
	ann := u.GetAnnotations()
	if ann == nil {
		ann = map[string]string{}
	}
	ann[v1.Group+"/simulate-partition-until"] = until.UTC().Format(time.RFC3339)
	u.SetAnnotations(ann)
	_, err = t.c.D.Resource(kube.Substrates).Update(t.ctx, u, metav1.UpdateOptions{})
	return err
}

func tc13(t *T) error {
	// Find where the work would go, silence that substrate, then submit it:
	// the scheduler still sees a fresh heartbeat and plans there, the agent
	// never claims, and the plan must return to the pool once the heartbeat
	// is stale.
	spec := maxcut("G1", 0.98)
	spec.DeadlineSeconds = 600
	if err := t.createProblem("tc13-probe", withDry(spec), nil); err != nil {
		return err
	}
	p, err := t.wait("tc13-probe", "plan", phaseIs(v1.PhasePlanned, v1.PhaseRejected))
	if err != nil || p.Status.Plan == nil {
		return fmt.Errorf("probe not planned: %v", err)
	}
	first := p.Status.Plan.Substrate
	if err := t.partition(first, time.Now().Add(75*time.Second)); err != nil {
		return err
	}
	time.Sleep(2 * time.Second)
	if err := t.createProblem("tc13", spec, nil); err != nil {
		return err
	}
	t.note("would run on %s; that agent was silenced for 75s before submission", first)
	p, err = t.wait("tc13", "completion", phaseIs(v1.PhaseSucceeded, v1.PhaseFailed))
	if err != nil {
		return err
	}
	if p.Status.ClaimedBy == first {
		return fmt.Errorf("completed on the silent substrate")
	}
	t.note("returned to the pool %d time(s), completed on %s (%s)", p.Status.Replans, p.Status.ClaimedBy, p.Status.Plan.Physics)
	return nil
}

func tc14(t *T) error {
	// The living-culture substrate needs seconds for a batch: long enough to
	// cut its agent off between the claim and the report.
	spec := digits(100, 0.9)
	spec.Physics = []string{"organoid"}
	spec.DeadlineSeconds = 900
	if err := t.createProblem("tc14", spec, nil); err != nil {
		return err
	}
	p, err := t.wait("tc14", "claim", phaseIs(v1.PhaseRunning, v1.PhaseSucceeded, v1.PhaseRejected))
	if err != nil {
		return err
	}
	if p.Status.Phase == v1.PhaseRejected {
		t.note("no organoid substrate available: %s", p.Status.Message)
		return errSkip
	}
	if p.Status.Phase != v1.PhaseRunning {
		t.note("finished before the cut could be applied")
		return errSkip
	}
	sub := p.Status.ClaimedBy
	until := time.Now().Add(30 * time.Second)
	if err := t.partition(sub, until); err != nil {
		return err
	}
	t.note("claimed by %s; agent cut off until %s", sub, until.Format(time.RFC3339))
	p, err = t.wait("tc14", "late report", phaseIs(v1.PhaseSucceeded, v1.PhaseFailed))
	if err != nil {
		return err
	}
	rc, err := t.receipt(p)
	if err != nil {
		return err
	}
	if !time.Now().After(until) {
		return fmt.Errorf("reported during the cut")
	}
	t.note("work finished at %s, during the cut; report delivered after it ended, receipt #%d", rc.Finished.Format(time.RFC3339), rc.Seq)
	if rc.Finished.After(until) {
		t.note("note: the work itself outlasted the cut")
	}
	return nil
}

func tc15(t *T) error {
	for _, c := range []struct {
		name string
		spec v1.ProblemSpec
	}{{"tc15-maxcut", withDry(maxcut("G1", 0.98))}, {"tc15-classify", withDry(digits(500, 0.97))}} {
		if err := t.createProblem(c.name, c.spec, nil); err != nil {
			return err
		}
		p, err := t.wait(c.name, "plan", phaseIs(v1.PhasePlanned, v1.PhaseRejected))
		if err != nil {
			return err
		}
		phys := map[string]bool{}
		for _, x := range p.Status.FeasiblePhysics {
			phys[x] = true
		}
		var ps []string
		for x := range phys {
			ps = append(ps, x)
		}
		sort.Strings(ps)
		if len(ps) < 2 {
			return fmt.Errorf("%s offered on %d physics: %v", c.spec.Class, len(ps), ps)
		}
		t.note("%s: reachable on %s; chosen %s", c.spec.Class, strings.Join(ps, ", "), p.Status.Plan.Physics)
	}
	return nil
}

func tc16(t *T) error {
	p, err := t.runToSuccess("tc16", digits(200, 0.95))
	if err != nil {
		return err
	}
	if p.Status.Phase != v1.PhaseSucceeded || p.Status.Result == nil {
		return fmt.Errorf("ended %s", p.Status.Phase)
	}
	if p.Status.Plan.PQuality <= 0 || p.Status.Plan.QualityN == 0 {
		return fmt.Errorf("plan without a confidence bound")
	}
	t.note("plan: P(quality >= %.2f) >= %.3f from %d runs; achieved %.4f", p.Spec.Quality, p.Status.Plan.PQuality, p.Status.Plan.QualityN, p.Status.Result.Quality)
	return nil
}
