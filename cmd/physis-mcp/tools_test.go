package main

import (
	"bufio"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	clienttesting "k8s.io/client-go/testing"

	v1 "github.com/kuberoot-dev/physis/api/v1alpha1"
	"github.com/kuberoot-dev/physis/pkg/kube"
	"github.com/kuberoot-dev/physis/pkg/kube/kubefake"
	"github.com/kuberoot-dev/physis/pkg/plan"
	"github.com/kuberoot-dev/physis/pkg/reason"
	"github.com/kuberoot-dev/physis/pkg/receipt"
)

var now = time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)

// session is an MCP client talking to a physis-mcp server over pipes, the
// way an agent's host does over stdio.
type session struct {
	t   *testing.T
	in  io.WriteCloser
	out *bufio.Reader
	id  int
}

func (s *session) call(method string, params any) map[string]any {
	s.t.Helper()
	s.id++
	b, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": s.id, "method": method, "params": params})
	if _, err := s.in.Write(append(b, '\n')); err != nil {
		s.t.Fatal(err)
	}
	line, err := s.out.ReadBytes('\n')
	if err != nil {
		s.t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(line, &m); err != nil {
		s.t.Fatalf("%v: %s", err, line)
	}
	if m["error"] != nil {
		s.t.Fatalf("%s: protocol error %v", method, m["error"])
	}
	return m["result"].(map[string]any)
}

// tool calls a tool and returns its structured result; failed reports
// whether the tool said isError, with the text.
func (s *session) tool(name string, args any) (res map[string]any, failed bool, text string) {
	s.t.Helper()
	r := s.call("tools/call", map[string]any{"name": name, "arguments": args})
	text = r["content"].([]any)[0].(map[string]any)["text"].(string)
	if r["isError"] == true {
		return nil, true, text
	}
	return r["structuredContent"].(map[string]any), false, text
}

func start(t *testing.T, objs ...runtime.Object) (*session, *tools) {
	t.Helper()
	c, fake := kubefake.New(objs...)
	// Stand in for the scheduler: a dry run is planned as soon as it is
	// created.
	fake.PrependReactor("create", "problems", func(a clienttesting.Action) (bool, runtime.Object, error) {
		u := a.(clienttesting.CreateAction).GetObject().(*unstructured.Unstructured)
		if dry, _, _ := unstructured.NestedBool(u.Object, "spec", "dryRun"); dry {
			u.Object["status"] = map[string]any{
				"phase":       v1.PhasePlanned,
				"message":     "dry run: plan only, nothing will run",
				"plan":        map[string]any{"substrate": "cpu-de", "zone": "DE", "physics": "cpu", "algorithm": "anneal", "joules": 12.5, "gco2": 0.001, "eur": nil},
				"reasons":     []any{"qpu-fr: zone FR not allowed"},
				"reasonCodes": []any{reason.ZoneNotAllowed},
			}
		}
		return false, nil, nil
	})
	tl := &tools{c: c, namespace: "agents", staleAfter: 30 * time.Second, planTimeout: 5 * time.Second, poll: 10 * time.Millisecond, now: func() time.Time { return now }}
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	done := make(chan struct{})
	go func() {
		tl.server().Serve(context.Background(), inR, outW)
		outW.Close()
		close(done)
	}()
	t.Cleanup(func() { inW.Close(); <-done })
	s := &session{t: t, in: inW, out: bufio.NewReader(outR)}
	init := s.call("initialize", map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{}, "clientInfo": map[string]any{"name": "test", "version": "0"}})
	if init["serverInfo"].(map[string]any)["name"] != "physis" {
		t.Fatalf("initialize: %v", init)
	}
	return s, tl
}

func encode[S, T any](t *testing.T, kind string, o *kube.Object[S, T]) runtime.Object {
	t.Helper()
	u, err := kube.Encode(kind, o)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func substrate(t *testing.T, name string, heartbeat time.Time, key string, retired ...string) runtime.Object {
	return encode(t, "Substrate", &kube.Substrate{Meta: metav1.ObjectMeta{Name: name},
		Spec: v1.SubstrateSpec{Physics: "cpu", Zone: "DE", Operator: "op", Jurisdiction: "EU", Slots: 2,
			Model: []v1.Parameter{{Name: "joulesPerOp", Value: 1e-9, Source: "ref:example"}}},
		Status: v1.SubstrateStatus{Heartbeat: heartbeat, PublicKey: key, RetiredKeys: retired}})
}

func digitsArgs() map[string]any {
	return map[string]any{"class": "classify", "instance": map[string]any{"digits": map[string]any{"size": 100, "offset": 0}},
		"quality": 0.95, "deadlineSeconds": 120, "policy": "min-energy"}
}

func TestToolsAreListedWithSchemas(t *testing.T) {
	s, _ := start(t)
	tools := s.call("tools/list", map[string]any{})["tools"].([]any)
	want := map[string][]string{"list_substrates": nil, "plan": {"class", "instance", "quality", "policy"},
		"submit": {"requestID", "class", "instance", "quality", "policy"}, "status": {"name"}, "verify_receipts": nil}
	if len(tools) != len(want) {
		t.Fatalf("%d tools, want %d", len(tools), len(want))
	}
	for _, x := range tools {
		tl := x.(map[string]any)
		req, ok := want[tl["name"].(string)]
		if !ok {
			t.Fatalf("unexpected tool %v", tl["name"])
		}
		sc := tl["inputSchema"].(map[string]any)
		if sc["type"] != "object" || sc["additionalProperties"] != false || tl["description"] == "" {
			t.Fatalf("%s: schema %v", tl["name"], sc)
		}
		if fmt.Sprint(sc["required"]) != fmt.Sprint(toAny(req)) && !(req == nil && sc["required"] == nil) {
			t.Fatalf("%s requires %v, want %v", tl["name"], sc["required"], req)
		}
	}
}

func toAny(xs []string) []any {
	var out []any
	for _, x := range xs {
		out = append(out, x)
	}
	return out
}

func TestListSubstratesReportsLivenessAndProvenance(t *testing.T) {
	s, _ := start(t, substrate(t, "cpu-de", now.Add(-5*time.Second), "k1"), substrate(t, "cpu-gone", now.Add(-time.Hour), "k2"))
	res, failed, text := s.tool("list_substrates", map[string]any{})
	if failed {
		t.Fatal(text)
	}
	subs := res["substrates"].([]any)
	if len(subs) != 2 {
		t.Fatalf("%d substrates", len(subs))
	}
	alive := map[string]bool{}
	for _, x := range subs {
		m := x.(map[string]any)
		alive[m["name"].(string)] = m["alive"].(bool)
		p := m["model"].([]any)[0].(map[string]any)
		if p["source"] != "ref:example" || m["jurisdiction"] != "EU" {
			t.Fatalf("substrate without provenance or jurisdiction: %v", m)
		}
	}
	if !alive["cpu-de"] || alive["cpu-gone"] {
		t.Fatalf("liveness: %v", alive)
	}
	res, _, _ = s.tool("list_substrates", map[string]any{"aliveOnly": true})
	if res["count"].(float64) != 1 {
		t.Fatalf("aliveOnly: %v", res)
	}
}

func TestPlanIsADryRunThatLeavesNothingBehind(t *testing.T) {
	s, tl := start(t)
	res, failed, text := s.tool("plan", digitsArgs())
	if failed {
		t.Fatal(text)
	}
	if res["phase"] != v1.PhasePlanned || res["dryRun"] != true || res["plan"].(map[string]any)["substrate"] != "cpu-de" {
		t.Fatalf("plan: %v", res)
	}
	if fmt.Sprint(res["reasonCodes"]) != fmt.Sprint([]any{reason.ZoneNotAllowed}) {
		t.Fatalf("reason codes: %v", res["reasonCodes"])
	}
	// An unknown price is null, never zero: a zone without a price must not
	// look free.
	if eur, ok := res["plan"].(map[string]any)["eur"]; !ok || eur != nil {
		t.Fatalf("unknown price came back as %v", eur)
	}
	l, err := tl.c.D.Resource(kube.Problems).Namespace("agents").List(context.Background(), metav1.ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(l.Items) != 0 {
		t.Fatalf("the dry run was left behind: %d problems", len(l.Items))
	}
	// Strict arguments: a misspelt constraint is refused, not dropped.
	args := digitsArgs()
	args["zone"] = "DE"
	if _, failed, text := s.tool("plan", args); !failed || !strings.Contains(text, "zone") {
		t.Fatalf("a misspelt field was accepted: %s", text)
	}
	delete(args, "zone")
	delete(args, "deadlineSeconds")
	if _, failed, text := s.tool("plan", args); !failed || !strings.Contains(text, "deadline") {
		t.Fatalf("a plan without a deadline was accepted: %s", text)
	}
}

func TestSubmitIsIdempotentAndStatusFollows(t *testing.T) {
	s, tl := start(t)
	args := digitsArgs()
	if _, failed, text := s.tool("submit", args); !failed || !strings.Contains(text, "requestID") {
		t.Fatalf("submit without a requestID: %s", text)
	}
	args["requestID"] = "agent-7/task-42"
	first, failed, text := s.tool("submit", args)
	if failed {
		t.Fatal(text)
	}
	if first["existing"] != false || first["phase"] != v1.PhasePending || first["name"] != nameFor("agent-7/task-42") {
		t.Fatalf("first submit: %v", first)
	}
	again, failed, text := s.tool("submit", args)
	if failed {
		t.Fatal(text)
	}
	if again["existing"] != true || again["name"] != first["name"] {
		t.Fatalf("a retry was not recognised: %v", again)
	}
	// The scheduler finishes the work; status reports the result and the
	// receipt.
	ctx := context.Background()
	p, err := tl.c.GetProblem(ctx, "agents", first["name"].(string))
	if err != nil {
		t.Fatal(err)
	}
	p.Status = v1.ProblemStatus{Phase: v1.PhaseSucceeded, ClaimedBy: "cpu-de", Plan: &plan.Candidate{Substrate: "cpu-de", EUR: math.NaN()},
		Result: &v1.Result{Quality: 0.97, Receipt: "rcpt-1", DeviceJoules: 3}}
	if _, err := tl.c.UpdateProblemStatus(ctx, p); err != nil {
		t.Fatal(err)
	}
	st, failed, text := s.tool("status", map[string]any{"name": first["name"]})
	if failed {
		t.Fatal(text)
	}
	if st["phase"] != v1.PhaseSucceeded || st["receipt"] != "rcpt-1" || st["requestID"] != "agent-7/task-42" {
		t.Fatalf("status: %v", st)
	}
	if _, failed, _ := s.tool("status", map[string]any{"name": "missing"}); !failed {
		t.Fatal("status of a missing problem succeeded")
	}
}

func TestVerifyReceiptsWithPublishedAndRetiredKeys(t *testing.T) {
	sign := func(n int) ([]*receipt.Receipt, string) {
		_, priv, _ := ed25519.GenerateKey(rand.Reader)
		sg := receipt.NewSigner(priv, 0, "")
		var rs []*receipt.Receipt
		for i := 0; i < n; i++ {
			r := &receipt.Receipt{Problem: "agents/p", Substrate: "cpu-de", Seed: 1794153289505270517, DeviceJoules: float64(i) + 0.25}
			sg.Sign(r)
			rs = append(rs, r)
		}
		return rs, sg.PublicKey()
	}
	old, oldKey := sign(2)
	cur, curKey := sign(3)
	objs := []runtime.Object{substrate(t, "cpu-de", now, curKey, oldKey)}
	for i, r := range append(old, cur...) {
		objs = append(objs, encode(t, "Receipt", &kube.Object[v1.ReceiptSpec, struct{}]{Meta: metav1.ObjectMeta{Name: fmt.Sprintf("r%d", i), Namespace: "agents"}, Spec: *r}))
	}
	s, _ := start(t, objs...)
	res, failed, text := s.tool("verify_receipts", map[string]any{"substrate": "cpu-de"})
	if failed {
		t.Fatal(text)
	}
	if res["ok"] != true || res["verified"].(float64) != 5 || len(res["chains"].([]any)) != 2 {
		t.Fatalf("verify: %s", text)
	}
	if _, failed, _ := s.tool("verify_receipts", map[string]any{"substrate": "nope"}); !failed {
		t.Fatal("an unknown substrate verified")
	}
}

func TestVerifyReceiptsCatchesAnUnpublishedKey(t *testing.T) {
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	sg := receipt.NewSigner(priv, 0, "")
	r := &receipt.Receipt{Problem: "agents/p", Substrate: "cpu-de"}
	sg.Sign(r)
	s, _ := start(t, substrate(t, "cpu-de", now, "someone-else"),
		encode(t, "Receipt", &kube.Object[v1.ReceiptSpec, struct{}]{Meta: metav1.ObjectMeta{Name: "r", Namespace: "agents"}, Spec: *r}))
	res, failed, text := s.tool("verify_receipts", map[string]any{})
	if failed {
		t.Fatal(text)
	}
	if res["ok"] != false || !strings.Contains(text, "never published") {
		t.Fatalf("a receipt under an unpublished key passed: %s", text)
	}
}
