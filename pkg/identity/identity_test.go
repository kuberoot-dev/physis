package identity

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	v1 "github.com/kuberoot-dev/physis/api/v1alpha1"
	"github.com/kuberoot-dev/physis/pkg/plan"
)

func newKey(t *testing.T) ed25519.PrivateKey {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return priv
}

func TestAddressRoundTrip(t *testing.T) {
	priv := newKey(t)
	id := Of(priv)
	addr := id.String()
	if !strings.HasPrefix(addr, Scheme) || addr != strings.ToLower(addr) {
		t.Fatalf("address %q", addr)
	}
	if len(addr) != len(Scheme)+55 {
		t.Fatalf("address %q has %d characters", addr, len(addr))
	}
	back, err := Parse(addr)
	if err != nil || back != id {
		t.Fatalf("Parse(%q) = %v, %v", addr, back, err)
	}
	if up, err := Parse(Scheme + strings.ToUpper(addr[len(Scheme):])); err != nil || up != id {
		t.Fatalf("upper case address: %v", err)
	}
	h, err := FromHex(id.Hex())
	if err != nil || h != id {
		t.Fatalf("FromHex: %v", err)
	}
	b, _ := json.Marshal(struct{ ID ID }{id})
	if !strings.Contains(string(b), addr) {
		t.Fatalf("JSON %s does not carry the address", b)
	}
	var v struct{ ID ID }
	if err := json.Unmarshal(b, &v); err != nil || v.ID != id {
		t.Fatalf("JSON round trip: %v", err)
	}
}

func TestAddressRejectsMalformed(t *testing.T) {
	addr := Of(newKey(t)).String()
	// Change one character of the key part: the checksum catches it.
	i := len(Scheme) + 5
	c := byte('a')
	if addr[i] == 'a' {
		c = 'b'
	}
	typo := addr[:i] + string(c) + addr[i+1:]
	for _, bad := range []string{
		"", addr[len(Scheme):], "spiffe:" + addr[len(Scheme):], addr + "a", addr[:len(addr)-1],
		Scheme + "!!!", typo,
	} {
		if _, err := Parse(bad); err == nil {
			t.Errorf("Parse(%q) accepted", bad)
		}
	}
	if _, err := FromHex("abcd"); err == nil {
		t.Error("short hex key accepted")
	}
}

func TestChallengeResponse(t *testing.T) {
	caller, callee, forger := newKey(t), newKey(t), newKey(t)
	n, err := NewNonce()
	if err != nil {
		t.Fatal(err)
	}
	sig := Respond(callee, Of(caller), n)
	if err := VerifyResponse(Of(callee), Of(caller), n, sig); err != nil {
		t.Fatalf("honest answer rejected: %v", err)
	}
	// A forger answering in the callee's name signs with its own key.
	if err := VerifyResponse(Of(callee), Of(caller), n, Respond(forger, Of(caller), n)); !errors.Is(err, ErrNotProven) {
		t.Fatalf("forged answer: %v", err)
	}
	// The answer is bound to the nonce and to the caller.
	other, _ := NewNonce()
	if VerifyResponse(Of(callee), Of(caller), other, sig) == nil {
		t.Fatal("answer accepted for another nonce")
	}
	if VerifyResponse(Of(callee), Of(forger), n, sig) == nil {
		t.Fatal("answer replayed to another caller")
	}
	if VerifyResponse(Of(callee), Of(caller), n, sig[:10]) == nil {
		t.Fatal("truncated signature accepted")
	}
	// A receipt-style signature of the same bytes is not an answer: the
	// purpose is part of what is signed.
	if VerifyResponse(Of(callee), Of(caller), n, ed25519.Sign(callee, n[:])) == nil {
		t.Fatal("raw signature of the nonce accepted")
	}
}

// dir is a Directory the tests change while agents run.
type dir struct {
	mu   sync.Mutex
	recs map[string]Record
}

func newDir() *dir { return &dir{recs: map[string]Record{}} }

func (d *dir) Records(context.Context) ([]Record, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	var out []Record
	for _, r := range d.recs {
		out = append(out, r)
	}
	return out, nil
}

func (d *dir) set(r Record) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.recs[r.Substrate] = r
}

func TestResolve(t *testing.T) {
	a, b, old := newKey(t), newKey(t), newKey(t)
	d := newDir()
	ra, ok := RecordOf("a", v1.SubstrateSpec{Zone: "DE", Jurisdiction: "EU"},
		v1.SubstrateStatus{PublicKey: Of(a).Hex(), RetiredKeys: []string{Of(old).Hex()},
			Endpoints: []v1.Endpoint{{Transport: v1.TransportHTTPS, Address: "10.0.0.1:7443"}}})
	if !ok {
		t.Fatal("RecordOf rejected a valid key")
	}
	d.set(ra)
	if _, ok := RecordOf("nokey", v1.SubstrateSpec{}, v1.SubstrateStatus{}); ok {
		t.Fatal("RecordOf accepted a substrate with no key")
	}
	r := &Resolver{Dir: d}
	got, err := r.Resolve(context.Background(), Of(a))
	if err != nil || got.Substrate != "a" || got.Jurisdiction != "EU" {
		t.Fatalf("Resolve: %+v, %v", got, err)
	}
	if ep, ok := got.Endpoint(v1.TransportHTTPS); !ok || ep.Address != "10.0.0.1:7443" {
		t.Fatalf("endpoint %+v", ep)
	}
	if _, err := r.Resolve(context.Background(), Of(b)); !errors.Is(err, ErrUnknown) {
		t.Fatalf("unknown identity: %v", err)
	}
	if _, err := r.Resolve(context.Background(), Of(old)); !errors.Is(err, ErrUnknown) {
		t.Fatalf("a retired key must not resolve: %v", err)
	}
	// Someone registers another substrate with a's public key.
	d.set(Record{ID: Of(a), Substrate: "copycat", Jurisdiction: "XX"})
	if _, err := r.Resolve(context.Background(), Of(a)); err == nil || !strings.Contains(err.Error(), "claimed by") {
		t.Fatalf("two claimants: %v", err)
	}
}

func TestAllowSend(t *testing.T) {
	de := Record{Substrate: "de", Zone: "DE", Jurisdiction: "EU"}
	fr := Record{Substrate: "fr", Zone: "FR", Jurisdiction: "EU"}
	gb := Record{Substrate: "gb", Zone: "GB", Jurisdiction: "UK"}
	de2 := Record{Substrate: "de2", Zone: "DE", Jurisdiction: "EU"}
	links := []plan.Link{
		{Name: "de-fr", From: "DE", To: "FR", Jurisdictions: []string{"EU"}},
		{Name: "gb-fr", From: "GB", To: "FR", Jurisdictions: []string{"UK", "EU"}},
	}
	for _, c := range []struct {
		name     string
		allowed  []string
		links    []plan.Link
		from, to Record
		ok       bool
	}{
		{"any jurisdiction, no links", nil, nil, de, gb, true},
		{"receiver allowed", []string{"EU"}, nil, de, fr, true},
		{"receiver not allowed", []string{"EU"}, nil, de, gb, false},
		{"link within allowed", []string{"EU"}, links, de, fr, true},
		{"path crosses UK", []string{"EU"}, links, gb, fr, false},
		{"path crosses UK, UK allowed", []string{"EU", "UK"}, links, gb, fr, true},
		{"no link between zones", nil, links, de, gb, false},
		{"same zone needs no link", []string{"EU"}, links, de, de2, true},
	} {
		err := AllowSend(c.allowed, c.links, c.from, c.to)
		if (err == nil) != c.ok {
			t.Errorf("%s: %v", c.name, err)
		}
		if err != nil && !errors.Is(err, ErrRefused) {
			t.Errorf("%s: %v is not a refusal", c.name, err)
		}
	}
}
