// Command physis-agent is the substrate half of physis. One agent stands for
// one substrate: it publishes the substrate's model, sends heartbeats, and
// claims the problems planned for it when their start time comes. It runs the
// work, signs a receipt chained to its previous ones, and reports the result.
//
// There is no kubelet and no Node object: the agent is an ordinary Deployment
// and pulls work (P7). If it loses the API server after claiming, it finishes
// the work, keeps the receipt in a local outbox on disk and delivers it when
// the API server is back (P8).
package main

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	v1 "github.com/kuberoot-dev/physis/api/v1alpha1"
	"github.com/kuberoot-dev/physis/pkg/device"
	"github.com/kuberoot-dev/physis/pkg/grid"
	"github.com/kuberoot-dev/physis/pkg/instances"
	"github.com/kuberoot-dev/physis/pkg/kube"
	"github.com/kuberoot-dev/physis/pkg/receipt"
	"github.com/kuberoot-dev/physis/pkg/run"
)

var (
	kubeconfig   = flag.String("kubeconfig", "", "kubeconfig (empty: in-cluster)")
	name         = flag.String("name", os.Getenv("PHYSIS_SUBSTRATE"), "substrate name")
	physics      = flag.String("physics", os.Getenv("PHYSIS_PHYSICS"), "physics of this substrate")
	zone         = flag.String("zone", os.Getenv("PHYSIS_ZONE"), "grid zone")
	operator     = flag.String("operator", os.Getenv("PHYSIS_OPERATOR"), "operator of the substrate")
	jurisdiction = flag.String("jurisdiction", os.Getenv("PHYSIS_JURISDICTION"), "jurisdiction of the operator")
	slots        = flag.Int("slots", 1, "parallel runs")
	dataDir      = flag.String("data", "/data", "directory with gset/ and digits/")
	stateDir     = flag.String("state", "/state", "directory for the signing key and the outbox")
	code         = flag.String("code", "physis-agent/dev", "build identifier written into receipts")
	gridCache    = flag.String("grid-cache", "", "directory caching live grid data (30 min)")
	keySecret    = flag.String("key-secret", "", "Secret in the agent's namespace that keeps the signing key across restarts (empty: the state directory only)")
	namespace    = flag.String("namespace", os.Getenv("POD_NAMESPACE"), "namespace of the key Secret")
	partitionAnn = v1.Group + "/simulate-partition-until"
)

type agent struct {
	c      *kube.Client
	model  *device.Model
	signer *receipt.Signer
	loader *instances.Loader
	sem    chan struct{}
	mu     sync.Mutex
	outbox []pending
	grid   *grid.Series
	best   map[string]float64
	cut    time.Time // simulated partition: no API calls before this
}

// pending is a finished run whose report has not reached the API server.
type pending struct {
	Namespace string          `json:"namespace"`
	Problem   string          `json:"problem"`
	Receipt   receipt.Receipt `json:"receipt"`
	Result    v1.Result       `json:"result"`
}

func main() {
	flag.Parse()
	if *name == "" || *physics == "" || *zone == "" {
		log.Fatal("name, physics and zone are required")
	}
	m := device.Builtin(device.Physics(*physics))
	if m == nil {
		log.Fatalf("unknown physics %q", *physics)
	}
	c, err := kube.New(*kubeconfig)
	if err != nil {
		log.Fatal(err)
	}
	a := &agent{c: c, model: m, loader: &instances.Loader{Dir: *dataDir}, sem: make(chan struct{}, *slots)}
	a.loadBestFound()
	ctx := context.Background()
	priv := a.key(ctx)
	seq, head := uint64(0), ""
	if cur, err := c.GetSubstrate(ctx, *name); err == nil && cur.Status.PublicKey == hex.EncodeToString(priv.Public().(ed25519.PublicKey)) {
		seq, head = cur.Status.ChainSeq, cur.Status.ChainHead
	}
	a.signer = receipt.NewSigner(priv, seq, head)
	a.loadOutbox()
	go a.refreshGrid()
	for {
		a.tick(ctx)
		time.Sleep(time.Second)
	}
}

// key loads or creates the agent's Ed25519 identity. The identity must
// outlive the process: a substrate that comes back with a new key starts a
// new chain, and its earlier receipts verify only against the retired key.
// The state directory keeps it across process restarts; a Secret, when
// named, keeps it across pod restarts and moves.
func (a *agent) key(ctx context.Context) ed25519.PrivateKey {
	p := filepath.Join(*stateDir, "agent.key")
	if b, err := os.ReadFile(p); err == nil && len(b) == ed25519.PrivateKeySize {
		return ed25519.PrivateKey(b)
	}
	var priv ed25519.PrivateKey
	if *keySecret != "" {
		k, err := a.c.SecretKey(ctx, *namespace, *keySecret)
		if err != nil {
			log.Fatalf("key secret %s/%s: %v", *namespace, *keySecret, err)
		}
		if len(k) == ed25519.PrivateKeySize {
			priv = ed25519.PrivateKey(k)
		}
	}
	if priv == nil {
		var err error
		if _, priv, err = ed25519.GenerateKey(rand.Reader); err != nil {
			log.Fatal(err)
		}
		if *keySecret != "" {
			if err := a.c.CreateSecretKey(ctx, *namespace, *keySecret, priv); err != nil {
				// Another start won the race: take its key, not ours.
				k, gerr := a.c.SecretKey(ctx, *namespace, *keySecret)
				if gerr != nil || len(k) != ed25519.PrivateKeySize {
					log.Fatalf("key secret %s/%s: %v", *namespace, *keySecret, err)
				}
				priv = ed25519.PrivateKey(k)
			}
		}
	}
	if err := os.MkdirAll(*stateDir, 0o700); err != nil {
		log.Fatal(err)
	}
	if err := os.WriteFile(p, priv, 0o600); err != nil {
		log.Fatal(err)
	}
	return priv
}

func (a *agent) loadBestFound() {
	a.best = map[string]float64{}
	b, err := os.ReadFile(filepath.Join(*dataDir, "best-found.json"))
	if err == nil {
		json.Unmarshal(b, &a.best)
	}
}

func (a *agent) refreshGrid() {
	for {
		p, err := grid.CachedLive(&http.Client{Timeout: 2 * time.Minute}, zoneSource(*zone), time.Now().UTC(), *gridCache, 30*time.Minute)
		if err == nil {
			a.mu.Lock()
			a.grid = p.Zones[*zone]
			a.mu.Unlock()
			time.Sleep(30 * time.Minute)
		} else {
			log.Printf("grid: %v; retrying in 5 min", err)
			time.Sleep(5 * time.Minute)
		}
	}
}

func zoneSource(z string) []grid.ZoneSource {
	for _, s := range grid.DefaultZones {
		if s.Zone == z {
			return []grid.ZoneSource{s}
		}
	}
	return nil
}

func (a *agent) partitioned() bool { return time.Now().Before(a.cut) }

func (a *agent) tick(ctx context.Context) {
	if a.partitioned() {
		return
	}
	sub, err := a.c.GetSubstrate(ctx, *name)
	if err == nil {
		// Test hook for the conformance suite: an annotation asks the agent
		// to behave as if the API server were unreachable until a time.
		if t, perr := time.Parse(time.RFC3339, sub.Meta.Annotations[partitionAnn]); perr == nil && time.Now().Before(t) {
			a.cut = t
			log.Printf("simulating a partition until %s", t.Format(time.RFC3339))
			return
		}
	}
	a.heartbeat(ctx)
	a.flush(ctx)
	probs, err := a.c.ListProblems(ctx)
	if err != nil {
		log.Printf("list problems: %v", err)
		return
	}
	now := time.Now().UTC()
	for _, p := range probs {
		if p.Status.Phase != v1.PhasePlanned || p.Spec.DryRun || p.Status.Plan == nil || p.Status.Plan.Substrate != *name || p.Status.Plan.Start.After(now) {
			continue
		}
		select {
		case a.sem <- struct{}{}:
		default:
			return // all slots busy
		}
		// Claim with optimistic concurrency: if anyone changed the problem
		// since we listed it, the update fails and we leave it.
		p.Status.Phase = v1.PhaseRunning
		p.Status.ClaimedBy = *name
		p.Status.ClaimedAt = &now
		claimed, err := a.c.UpdateProblemStatus(ctx, p)
		if err != nil {
			<-a.sem
			continue
		}
		go func() {
			defer func() { <-a.sem }()
			a.execute(ctx, claimed)
		}()
	}
}

func (a *agent) heartbeat(ctx context.Context) {
	seq, head := a.signer.Head()
	s := &kube.Substrate{Spec: v1.SubstrateSpec{Physics: *physics, Zone: *zone, Operator: *operator, Jurisdiction: *jurisdiction, Emulated: a.model.Emulated, Slots: *slots, Model: params(a.model)},
		Status: v1.SubstrateStatus{Agent: hostname(), Heartbeat: time.Now().UTC(), PublicKey: a.signer.PublicKey(), ChainSeq: seq, ChainHead: head, Running: len(a.sem)}}
	s.Meta.Name = *name
	if cur, err := a.c.GetSubstrate(ctx, *name); err == nil {
		s.Meta = cur.Meta
		s.Status.RetiredKeys = cur.Status.RetiredKeys
		if cur.Status.PublicKey != "" && cur.Status.PublicKey != s.Status.PublicKey {
			// Record the change instead of overwriting: receipts signed with
			// the old key must stay verifiable.
			log.Printf("warning: substrate %s was registered with another key; taking it over and retiring the old one", *name)
			if !slices.Contains(s.Status.RetiredKeys, cur.Status.PublicKey) {
				s.Status.RetiredKeys = append(s.Status.RetiredKeys, cur.Status.PublicKey)
			}
		}
	}
	if _, err := a.c.ApplySubstrate(ctx, s); err != nil {
		log.Printf("heartbeat: %v", err)
	}
}

func params(m *device.Model) []v1.Parameter {
	p := func(n string, v device.Value) v1.Parameter {
		return v1.Parameter{Name: n, Value: v.V, Lo: v.Lo, Hi: v.Hi, Source: v.Source, Note: v.Note}
	}
	var out []v1.Parameter
	for _, x := range []struct {
		n string
		v device.Value
	}{{"opsPerSecond", m.OpsPerSecond}, {"energyPerOpJ", m.EnergyPerOp}, {"activePowerW", m.ActivePowerW}, {"setupSeconds", m.SetupSeconds}, {"setupJ", m.SetupJoules}} {
		if x.v.V != 0 {
			out = append(out, p(x.n, x.v))
		}
	}
	return out
}

func hostname() string { h, _ := os.Hostname(); return h }

func (a *agent) execute(ctx context.Context, p *kube.Problem) {
	pl := p.Status.Plan
	in, err := a.loader.Load(p.Spec.Instance, a.best)
	if err != nil {
		a.fail(ctx, p, err)
		return
	}
	started := time.Now().UTC()
	seed := uint64(started.UnixNano())
	res, err := run.Execute(a.model, pl.Algorithm, pl.Effort, in, seed)
	if err != nil {
		a.fail(ctx, p, err)
		return
	}
	// An emulated substrate is occupied for as long as the real one would
	// be: the emulator usually finishes sooner, so the agent holds the slot
	// until the device time has passed (capped at an hour).
	if a.model.Emulated {
		hold := time.Duration(res.Device.Seconds * float64(time.Second))
		if hold > time.Hour {
			hold = time.Hour
		}
		if d := time.Until(started.Add(hold)); d > 0 {
			time.Sleep(d)
		}
	}
	finished := time.Now().UTC()
	r := receipt.Receipt{Problem: p.Meta.Namespace + "/" + p.Meta.Name, Requester: p.Meta.Annotations[v1.Group+"/requester"], Mandate: p.Spec.Mandate,
		InstanceDigest: in.Digest(), Algorithm: pl.Algorithm, Effort: pl.Effort, Code: *code, Seed: seed,
		Substrate: *name, Physics: *physics, Emulated: a.model.Emulated, Zone: *zone,
		ResultDigest: res.ResultDigest, Quality: res.Quality, Value: res.Value,
		Started: started, Finished: finished, SpinUpdates: res.Ops.SpinUpdates, MACs: res.Ops.MACs,
		DeviceSeconds: res.Device.Seconds, DeviceJoules: res.Device.Joules, HostCPUSeconds: res.HostCPUSeconds, EnergySource: "model"}
	a.mu.Lock()
	if s := a.grid; s != nil {
		d := time.Duration(res.Device.Seconds * float64(time.Second))
		r.CI = s.CarbonActual(started, d)
		r.GCO2 = res.Device.Joules / 3.6e6 * r.CI
		if price := s.PriceAt(started, d); price == price {
			r.EUR = res.Device.Joules / 3.6e9 * price
		}
	}
	a.signer.Sign(&r)
	pend := pending{Namespace: p.Meta.Namespace, Problem: p.Meta.Name, Receipt: r,
		Result: v1.Result{Quality: res.Quality, Value: res.Value, ResultDigest: res.ResultDigest, Receipt: receiptName(p.Meta.Name, r.Seq), DeviceJoules: res.Device.Joules, GCO2: r.GCO2}}
	a.outbox = append(a.outbox, pend)
	a.saveOutboxLocked()
	a.mu.Unlock()
	log.Printf("%s/%s done: %s/%s quality %.4f, device %.3gs %.3gJ, host CPU %.3gs", p.Meta.Namespace, p.Meta.Name, pl.Algorithm, pl.Effort, res.Quality, res.Device.Seconds, res.Device.Joules, res.HostCPUSeconds)
	a.flush(ctx)
}

func receiptName(problem string, seq uint64) string {
	return fmt.Sprintf("%s-%s-%d", problem, *name, seq)
}

// flush delivers finished reports in order. A report stays in the outbox
// until both the receipt and the problem status are written.
func (a *agent) flush(ctx context.Context) {
	if a.partitioned() {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	for len(a.outbox) > 0 {
		x := a.outbox[0]
		if err := a.c.CreateReceipt(ctx, x.Namespace, x.Result.Receipt, &x.Receipt); err != nil && !alreadyExists(err) {
			log.Printf("outbox: receipt %s: %v (will retry)", x.Result.Receipt, err)
			return
		}
		u, err := a.c.D.Resource(kube.Problems).Namespace(x.Namespace).Get(ctx, x.Problem, metav1.GetOptions{})
		if err == nil {
			p, derr := kube.Decode[v1.ProblemSpec, v1.ProblemStatus](u)
			if derr == nil {
				res := x.Result
				p.Status.Phase = v1.PhaseSucceeded
				p.Status.Result = &res
				_, err = a.c.UpdateProblemStatus(ctx, p)
			} else {
				err = derr
			}
		}
		if err != nil && !notFound(err) {
			log.Printf("outbox: status of %s/%s: %v (will retry)", x.Namespace, x.Problem, err)
			return
		}
		a.outbox = a.outbox[1:]
		a.saveOutboxLocked()
	}
}

func (a *agent) fail(ctx context.Context, p *kube.Problem, err error) {
	log.Printf("%s/%s failed: %v", p.Meta.Namespace, p.Meta.Name, err)
	p.Status.Phase = v1.PhaseFailed
	p.Status.Message = err.Error()
	a.c.UpdateProblemStatus(ctx, p)
}

func (a *agent) saveOutboxLocked() {
	b, _ := json.Marshal(a.outbox)
	tmp := filepath.Join(*stateDir, "outbox.json.tmp")
	if err := os.WriteFile(tmp, b, 0o600); err == nil {
		os.Rename(tmp, filepath.Join(*stateDir, "outbox.json"))
	}
}

func (a *agent) loadOutbox() {
	b, err := os.ReadFile(filepath.Join(*stateDir, "outbox.json"))
	if err == nil {
		json.Unmarshal(b, &a.outbox)
	}
}

func alreadyExists(err error) bool { return err != nil && contains(err.Error(), "already exists") }
func notFound(err error) bool      { return err != nil && contains(err.Error(), "not found") }

func contains(s, sub string) bool {
	return len(sub) <= len(s) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()
}
