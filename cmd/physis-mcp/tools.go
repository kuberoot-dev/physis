package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	v1 "github.com/kuberoot-dev/physis/api/v1alpha1"
	"github.com/kuberoot-dev/physis/pkg/kube"
	"github.com/kuberoot-dev/physis/pkg/mcp"
	"github.com/kuberoot-dev/physis/pkg/plan"
	"github.com/kuberoot-dev/physis/pkg/reason"
	"github.com/kuberoot-dev/physis/pkg/receipt"
)

// LabelVia marks problems created through this server.
const LabelVia = v1.Group + "/via"

type tools struct {
	c           *kube.Client
	namespace   string
	staleAfter  time.Duration
	planTimeout time.Duration
	poll        time.Duration
	now         func() time.Time
}

const instructions = `physis plans computation by outcome: state the quality, deadline, budget and allowed zones and jurisdictions, and it chooses the algorithm, physics, place and start time.
Use list_substrates to see what exists, plan to get a plan and its alternatives without running anything, submit to run (always with a requestID you reuse on retries), status to follow a problem, and verify_receipts to check the signed record of what ran.
Refusals carry reasonCodes; branch on them, not on the text: ` + "%s" + `.`

func (t *tools) server() *mcp.Server {
	return &mcp.Server{
		Name:         "physis",
		Version:      version,
		Instructions: fmt.Sprintf(instructions, strings.Join(reason.All(), ", ")),
		Tools: []mcp.Tool{
			{
				Name:  "list_substrates",
				Title: "List substrates",
				Description: "Lists the computing substrates: physics, zone, operator jurisdiction, whether emulated, whether alive, " +
					"published signing keys, and every device-model parameter with its provenance (measured, ref:<source> or assumption).",
				InputSchema: object(map[string]any{
					"physics":   str("only substrates of this physics"),
					"aliveOnly": map[string]any{"type": "boolean", "description": "only substrates whose agent sent a heartbeat recently"},
				}),
				Handler: t.listSubstrates,
			},
			{
				Name:  "plan",
				Title: "Plan without running",
				Description: "Dry run: asks the scheduler for a plan for an outcome and returns the chosen candidate (substrate, algorithm, start, " +
					"predicted seconds, joules, gCO2e, euros, probability of reaching the quality), the Pareto alternatives, the physics on which " +
					"the outcome is reachable, and reasons with reasonCodes for what was rejected. Nothing runs and nothing is spent.",
				InputSchema: object(merge(specProperties(), map[string]any{
					"timeoutSeconds": map[string]any{"type": "number", "minimum": 1, "maximum": 300, "description": "how long to wait for the scheduler"},
					"keep":           map[string]any{"type": "boolean", "description": "keep the dry-run Problem object instead of deleting it after reading"},
				}), "class", "instance", "quality", "policy"),
				Handler: t.plan,
			},
			{
				Name:  "submit",
				Title: "Submit a problem",
				Description: "Creates a Problem that will run. requestID is required and makes the call idempotent: retrying with the same " +
					"requestID returns the existing problem instead of running the work twice, and a copy created under another name is " +
					"rejected with reasonCode Duplicate. Returns the problem's name; follow it with status.",
				InputSchema: object(merge(specProperties(), map[string]any{
					"requestID": map[string]any{"type": "string", "minLength": 1, "maxLength": 253, "description": "idempotency key, unique per intended run; reuse it on retries"},
					"name":      str("object name; default derived from requestID"),
				}), "requestID", "class", "instance", "quality", "policy"),
				Handler: t.submit,
			},
			{
				Name:  "status",
				Title: "Problem status",
				Description: "Returns a problem's phase (Pending, Planned, AwaitingApproval, Running, Succeeded, Rejected, Failed), message, " +
					"reasons and reasonCodes, duplicateOf, the plan, who claimed it, the result and the name of its signed receipt.",
				InputSchema: object(map[string]any{
					"name":      str("problem name"),
					"namespace": str("namespace; default the server's"),
				}, "name"),
				Handler: t.status,
			},
			{
				Name:  "verify_receipts",
				Title: "Verify receipts",
				Description: "Fetches receipts and verifies them here, offline, against the keys the substrates publish (current and retired): " +
					"Ed25519 signatures, sequence numbers and hash links per substrate and key. A receipt signed with a key the substrate " +
					"never published fails.",
				InputSchema: object(map[string]any{
					"substrate": str("only this substrate's receipts; default all"),
					"namespace": str("only receipts in this namespace; default every namespace the caller can read"),
				}),
				Handler: t.verifyReceipts,
			},
		},
	}
}

// --- schemas ---------------------------------------------------------------

func object(props map[string]any, required ...string) json.RawMessage {
	m := map[string]any{"type": "object", "properties": props, "additionalProperties": false}
	if len(required) > 0 {
		m["required"] = required
	}
	b, err := json.Marshal(m)
	if err != nil {
		panic(err)
	}
	return b
}

func str(desc string) map[string]any { return map[string]any{"type": "string", "description": desc} }

func strs(desc string) map[string]any {
	return map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": desc}
}

func num(desc string) map[string]any { return map[string]any{"type": "number", "description": desc} }

func merge(a, b map[string]any) map[string]any {
	for k, v := range b {
		a[k] = v
	}
	return a
}

// specProperties is the schema of a problem stated as an outcome; it
// mirrors the Problem CRD.
func specProperties() map[string]any {
	return map[string]any{
		"namespace": str("namespace; default the server's"),
		"class":     map[string]any{"type": "string", "enum": []string{"maxcut", "classify"}},
		"instance": map[string]any{
			"type": "object", "additionalProperties": false, "minProperties": 1, "maxProperties": 1,
			"description": "exactly one of gset, random, digits",
			"properties": map[string]any{
				"gset": str("a Gset graph, e.g. G1"),
				"random": map[string]any{"type": "object", "required": []string{"n", "p", "seed"}, "additionalProperties": false,
					"properties": map[string]any{"n": map[string]any{"type": "integer", "minimum": 2}, "p": num("edge probability"), "seed": map[string]any{"type": "integer"}}},
				"digits": map[string]any{"type": "object", "required": []string{"size", "offset"}, "additionalProperties": false,
					"properties": map[string]any{"size": map[string]any{"type": "integer", "minimum": 1}, "offset": map[string]any{"type": "integer", "minimum": 0}}},
			},
		},
		"quality":         map[string]any{"type": "number", "minimum": 0, "maximum": 1.5, "description": "target: cut / reference cut, or accuracy / exact accuracy"},
		"confidence":      map[string]any{"type": "number", "minimum": 0, "maximum": 1, "description": "required lower bound on P(quality >= target); default 0.9"},
		"deadlineSeconds": map[string]any{"type": "integer", "minimum": 1, "description": "deadline relative to creation"},
		"deadline":        map[string]any{"type": "string", "format": "date-time", "description": "absolute deadline (RFC 3339)"},
		"deferrable":      map[string]any{"type": "boolean", "description": "may wait for cleaner or cheaper hours before the deadline"},
		"policy":          map[string]any{"type": "string", "enum": []string{string(plan.MinCarbon), string(plan.MinCost), string(plan.MinEnergy), string(plan.Fastest)}},
		"budget": map[string]any{"type": "object", "additionalProperties": false, "description": "hard caps; zero or absent means none",
			"properties": map[string]any{"joules": num("energy"), "eur": num("money"), "gco2": num("grams of CO2e")}},
		"zones":         strs("allowed zones; empty means any"),
		"jurisdictions": strs("allowed operator and data-path jurisdictions; empty means any"),
		"dataZone":      str("zone where the input data lives"),
		"dataBytes":     num("size of the input data to move"),
		"mandate":       str("name of a signed human mandate in the namespace that covers this work"),
		"physics":       strs("restrict to these physics (e.g. cpu, gpu, qpu); empty means any"),
	}
}

// --- arguments --------------------------------------------------------------

type specArgs struct {
	Namespace string `json:"namespace,omitempty"`
	v1.ProblemSpec
}

func (a *specArgs) validate() error {
	s := &a.ProblemSpec
	var errs []string
	if s.Class != "maxcut" && s.Class != "classify" {
		errs = append(errs, "class must be maxcut or classify")
	}
	n := 0
	if s.Instance.Gset != "" {
		n++
	}
	if s.Instance.Random != nil {
		n++
	}
	if s.Instance.Digits != nil {
		n++
	}
	if n != 1 {
		errs = append(errs, "instance must set exactly one of gset, random, digits")
	}
	if s.Quality <= 0 {
		errs = append(errs, "quality must be positive")
	}
	switch plan.Policy(s.Policy) {
	case plan.MinCarbon, plan.MinCost, plan.MinEnergy, plan.Fastest:
	default:
		errs = append(errs, "policy must be min-carbon, min-cost, min-energy or fastest")
	}
	if s.Deadline == nil && s.DeadlineSeconds <= 0 {
		errs = append(errs, "deadline or deadlineSeconds is required")
	}
	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}

func (t *tools) ns(n string) string {
	if n != "" {
		return n
	}
	return t.namespace
}

// --- views -----------------------------------------------------------------

type problemView struct {
	Namespace       string           `json:"namespace"`
	Name            string           `json:"name"`
	RequestID       string           `json:"requestID,omitempty"`
	DryRun          bool             `json:"dryRun,omitempty"`
	Phase           string           `json:"phase"`
	Message         string           `json:"message,omitempty"`
	Reasons         []string         `json:"reasons,omitempty"`
	ReasonCodes     []string         `json:"reasonCodes,omitempty"`
	DuplicateOf     string           `json:"duplicateOf,omitempty"`
	Plan            *plan.Candidate  `json:"plan,omitempty"`
	Pareto          []plan.Candidate `json:"pareto,omitempty"`
	Candidates      int              `json:"candidates,omitempty"`
	FeasiblePhysics []string         `json:"feasiblePhysics,omitempty"`
	Replans         int              `json:"replans,omitempty"`
	ClaimedBy       string           `json:"claimedBy,omitempty"`
	ClaimedAt       *time.Time       `json:"claimedAt,omitempty"`
	Result          *v1.Result       `json:"result,omitempty"`
	Receipt         string           `json:"receipt,omitempty"`
}

func view(p *kube.Problem) problemView {
	st := p.Status
	phase := st.Phase
	if phase == "" {
		phase = v1.PhasePending
	}
	v := problemView{Namespace: p.Meta.Namespace, Name: p.Meta.Name, RequestID: p.Spec.RequestID, DryRun: p.Spec.DryRun,
		Phase: phase, Message: st.Message, Reasons: st.Reasons, ReasonCodes: st.ReasonCodes, DuplicateOf: st.DuplicateOf,
		Plan: st.Plan, Pareto: st.Pareto, Candidates: st.Candidates, FeasiblePhysics: st.FeasiblePhysics, Replans: st.Replans,
		ClaimedBy: st.ClaimedBy, ClaimedAt: st.ClaimedAt, Result: st.Result}
	if st.Result != nil {
		v.Receipt = st.Result.Receipt
	}
	return v
}

// --- handlers ----------------------------------------------------------------

type substrateView struct {
	Name         string         `json:"name"`
	Physics      string         `json:"physics"`
	Zone         string         `json:"zone"`
	Operator     string         `json:"operator,omitempty"`
	Jurisdiction string         `json:"jurisdiction,omitempty"`
	Emulated     bool           `json:"emulated"`
	Slots        int            `json:"slots,omitempty"`
	Alive        bool           `json:"alive"`
	Heartbeat    *time.Time     `json:"heartbeat,omitempty"`
	Running      int            `json:"running,omitempty"`
	PublicKey    string         `json:"publicKey,omitempty"`
	RetiredKeys  []string       `json:"retiredKeys,omitempty"`
	ChainSeq     uint64         `json:"chainSeq,omitempty"`
	Model        []v1.Parameter `json:"model"`
}

func (t *tools) listSubstrates(ctx context.Context, raw json.RawMessage) (any, error) {
	var a struct {
		Physics   string `json:"physics"`
		AliveOnly bool   `json:"aliveOnly"`
	}
	if err := mcp.DecodeArgs(raw, &a); err != nil {
		return nil, err
	}
	subs, err := t.c.ListSubstrates(ctx)
	if err != nil {
		return nil, err
	}
	now := t.now()
	out := []substrateView{}
	for _, s := range subs {
		alive := !s.Status.Heartbeat.IsZero() && now.Sub(s.Status.Heartbeat) < t.staleAfter
		if (a.Physics != "" && s.Spec.Physics != a.Physics) || (a.AliveOnly && !alive) {
			continue
		}
		v := substrateView{Name: s.Meta.Name, Physics: s.Spec.Physics, Zone: s.Spec.Zone, Operator: s.Spec.Operator, Jurisdiction: s.Spec.Jurisdiction,
			Emulated: s.Spec.Emulated, Slots: s.Spec.Slots, Alive: alive, Running: s.Status.Running, PublicKey: s.Status.PublicKey,
			RetiredKeys: s.Status.RetiredKeys, ChainSeq: s.Status.ChainSeq, Model: s.Spec.Model}
		if !s.Status.Heartbeat.IsZero() {
			hb := s.Status.Heartbeat
			v.Heartbeat = &hb
		}
		out = append(out, v)
	}
	return map[string]any{"substrates": out, "count": len(out), "at": now.UTC()}, nil
}

func (t *tools) plan(ctx context.Context, raw json.RawMessage) (any, error) {
	var a struct {
		specArgs
		TimeoutSeconds float64 `json:"timeoutSeconds,omitempty"`
		Keep           bool    `json:"keep,omitempty"`
	}
	if err := mcp.DecodeArgs(raw, &a); err != nil {
		return nil, err
	}
	if err := a.validate(); err != nil {
		return nil, err
	}
	if a.RequestID != "" {
		return nil, errors.New("plan does not take a requestID: a dry run runs nothing; pass it to submit")
	}
	a.DryRun = true
	suffix := make([]byte, 6)
	rand.Read(suffix)
	p := &kube.Problem{Meta: metav1.ObjectMeta{Name: "dryrun-" + hex.EncodeToString(suffix), Namespace: t.ns(a.Namespace),
		Labels: map[string]string{LabelVia: "mcp"}}, Spec: a.ProblemSpec}
	if _, err := t.c.CreateProblem(ctx, p); err != nil {
		return nil, err
	}
	if !a.Keep {
		// The dry run is read once; do not leave it behind, even when the
		// caller has gone.
		defer t.c.D.Resource(kube.Problems).Namespace(p.Meta.Namespace).Delete(context.WithoutCancel(ctx), p.Meta.Name, metav1.DeleteOptions{})
	}
	timeout := t.planTimeout
	if a.TimeoutSeconds > 0 {
		timeout = time.Duration(min(a.TimeoutSeconds, 300) * float64(time.Second))
	}
	wctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	for {
		got, err := t.c.GetProblem(wctx, p.Meta.Namespace, p.Meta.Name)
		if err == nil && got.Status.Phase != "" && got.Status.Phase != v1.PhasePending {
			v := view(got)
			return struct {
				problemView
				Kept bool `json:"kept"`
			}{v, a.Keep}, nil
		}
		select {
		case <-wctx.Done():
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, fmt.Errorf("no plan within %s: is physis-scheduler running?", timeout)
		case <-time.After(t.poll):
		}
	}
}

// nameFor derives an object name from a requestID, so that a retry of the
// same submission collides with the first one at the API server.
func nameFor(requestID string) string {
	sum := sha256.Sum256([]byte(requestID))
	return "req-" + hex.EncodeToString(sum[:10])
}

func (t *tools) submit(ctx context.Context, raw json.RawMessage) (any, error) {
	var a struct {
		specArgs
		Name string `json:"name,omitempty"`
	}
	if err := mcp.DecodeArgs(raw, &a); err != nil {
		return nil, err
	}
	if a.RequestID == "" {
		return nil, errors.New("requestID is required: choose one per intended run and reuse it when retrying")
	}
	if a.DryRun {
		return nil, errors.New("submit runs work; use plan for a dry run")
	}
	if err := a.validate(); err != nil {
		return nil, err
	}
	name := a.Name
	if name == "" {
		name = nameFor(a.RequestID)
	}
	p := &kube.Problem{Meta: metav1.ObjectMeta{Name: name, Namespace: t.ns(a.Namespace), Labels: map[string]string{LabelVia: "mcp"}}, Spec: a.ProblemSpec}
	type submitted struct {
		problemView
		Existing bool `json:"existing"`
	}
	created, err := t.c.CreateProblem(ctx, p)
	if apierrors.IsAlreadyExists(err) {
		cur, gerr := t.c.GetProblem(ctx, p.Meta.Namespace, name)
		if gerr != nil {
			return nil, gerr
		}
		if cur.Spec.RequestID != a.RequestID {
			return nil, fmt.Errorf("name %s/%s is taken by a problem with requestID %q", p.Meta.Namespace, name, cur.Spec.RequestID)
		}
		return submitted{view(cur), true}, nil
	}
	if err != nil {
		return nil, err
	}
	return submitted{view(created), false}, nil
}

func (t *tools) status(ctx context.Context, raw json.RawMessage) (any, error) {
	var a struct {
		Name      string `json:"name"`
		Namespace string `json:"namespace"`
	}
	if err := mcp.DecodeArgs(raw, &a); err != nil {
		return nil, err
	}
	if a.Name == "" {
		return nil, errors.New("name is required")
	}
	p, err := t.c.GetProblem(ctx, t.ns(a.Namespace), a.Name)
	if err != nil {
		return nil, err
	}
	return view(p), nil
}

func (t *tools) verifyReceipts(ctx context.Context, raw json.RawMessage) (any, error) {
	var a struct {
		Substrate string `json:"substrate"`
		Namespace string `json:"namespace"`
	}
	if err := mcp.DecodeArgs(raw, &a); err != nil {
		return nil, err
	}
	subs, err := t.c.ListSubstrates(ctx)
	if err != nil {
		return nil, err
	}
	keys := map[string][]string{}
	for _, s := range subs {
		keys[s.Meta.Name] = append([]string{s.Status.PublicKey}, s.Status.RetiredKeys...)
	}
	if _, ok := keys[a.Substrate]; a.Substrate != "" && !ok {
		return nil, fmt.Errorf("no substrate %q", a.Substrate)
	}
	all, err := t.c.ListReceipts(ctx, a.Namespace)
	if err != nil {
		return nil, err
	}
	var rs []*receipt.Receipt
	for _, r := range all {
		if a.Substrate == "" || r.Substrate == a.Substrate {
			rs = append(rs, r)
		}
	}
	reps := receipt.CheckChains(rs, keys)
	ok, verified := true, 0
	for _, c := range reps {
		ok = ok && c.OK()
		verified += c.Verified
	}
	if reps == nil {
		reps = []receipt.ChainReport{}
	}
	return map[string]any{"ok": ok, "receipts": len(rs), "verified": verified, "chains": reps}, nil
}
