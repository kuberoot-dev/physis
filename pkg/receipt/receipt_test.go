package receipt

import (
	"crypto/ed25519"
	"crypto/rand"
	"strings"
	"testing"
)

func chain(t *testing.T, n int) ([]*Receipt, string) {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	s := NewSigner(priv, 0, "")
	var rs []*Receipt
	for i := 0; i < n; i++ {
		r := &Receipt{Problem: "ns/p", Algorithm: "anneal", Quality: 0.99, DeviceJoules: float64(i)}
		s.Sign(r)
		rs = append(rs, r)
	}
	return rs, s.PublicKey()
}

func TestChainVerifies(t *testing.T) {
	rs, key := chain(t, 5)
	if err := VerifyChain(rs, key); err != nil {
		t.Fatal(err)
	}
}

func TestEditedReceiptIsDetected(t *testing.T) {
	rs, key := chain(t, 5)
	rs[2].DeviceJoules = 0.001 // understate the energy after the fact
	if err := VerifyChain(rs, key); err == nil || !strings.Contains(err.Error(), "bad signature") {
		t.Fatalf("edit not detected: %v", err)
	}
}

func TestRemovedReceiptIsDetected(t *testing.T) {
	rs, key := chain(t, 5)
	rs = append(rs[:2], rs[3:]...)
	if err := VerifyChain(rs, key); err == nil || !strings.Contains(err.Error(), "missing") {
		t.Fatalf("removal not detected: %v", err)
	}
}

func TestReceiptFromAnotherAgentIsRejected(t *testing.T) {
	rs, _ := chain(t, 2)
	other, _ := chain(t, 1)
	if err := VerifyChain(rs, other[0].Key); err == nil {
		t.Fatal("receipts accepted under the wrong key")
	}
}

func TestResignedForgeryBreaksTheLink(t *testing.T) {
	rs, key := chain(t, 3)
	// A forger without the key cannot re-sign; with a different key the
	// trusted-key check fails; re-signing the middle one with the right key
	// (a compromised agent) changes its hash and breaks the next link.
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	f := NewSigner(priv, rs[1].Seq-1, rs[1].Prev)
	forged := *rs[1]
	forged.DeviceJoules = 0
	f.Sign(&forged)
	rs[1] = &forged
	if err := VerifyChain(rs, key); err == nil {
		t.Fatal("forged receipt accepted")
	}
}

// substrateChain signs n receipts of one substrate with a fresh key.
func substrateChain(t *testing.T, sub string, n int) ([]*Receipt, string) {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	s := NewSigner(priv, 0, "")
	var rs []*Receipt
	for i := 0; i < n; i++ {
		r := &Receipt{Problem: "ns/p", Substrate: sub, Quality: 0.9, DeviceJoules: float64(i + 1)}
		s.Sign(r)
		rs = append(rs, r)
	}
	return rs, s.PublicKey()
}

func TestCheckChainsUsesPublishedAndRetiredKeys(t *testing.T) {
	old, oldKey := substrateChain(t, "sub-a", 3)
	cur, curKey := substrateChain(t, "sub-a", 4)
	stray, strayKey := substrateChain(t, "sub-a", 2)
	// The first receipt of the current chain is gone: only the tail after
	// the gap is judged.
	all := append(append(append([]*Receipt{}, old...), cur[1:]...), stray...)
	keys := map[string][]string{"sub-a": {curKey, oldKey}}
	reps := CheckChains(all, keys)
	if len(reps) != 3 {
		t.Fatalf("%d chains, want 3", len(reps))
	}
	byKey := map[string]ChainReport{}
	for _, r := range reps {
		byKey[r.Key] = r
	}
	if r := byKey[oldKey]; !r.OK() || r.Verified != 3 {
		t.Fatalf("retired-key chain: %+v", r)
	}
	if r := byKey[curKey]; !r.OK() || r.Verified != 3 || r.FirstSeq != 2 {
		t.Fatalf("current-key chain after a missing first receipt: %+v", r)
	}
	if r := byKey[strayKey]; r.OK() || r.Published || !strings.Contains(r.Error, "never published") {
		t.Fatalf("a chain under an unpublished key passed: %+v", r)
	}
	forged := *old[1]
	forged.DeviceJoules /= 10
	if r := CheckChains([]*Receipt{old[0], &forged, old[2]}, keys); r[0].OK() {
		t.Fatal("a tampered receipt verified")
	}
}
