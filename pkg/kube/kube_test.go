package kube

import (
	"crypto/ed25519"
	"crypto/rand"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kuberoot-dev/physis/pkg/receipt"
)

// A receipt must survive the trip to unstructured and back bit for bit, or
// its signature stops verifying. 64-bit seeds are the case that broke it.
func TestReceiptRoundTripKeepsSignatureValid(t *testing.T) {
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	s := receipt.NewSigner(priv, 0, "")
	r := &receipt.Receipt{Problem: "ns/p", Seed: 1794153289505270517, DeviceJoules: 3.3727999999961747e-07, Quality: 0.9777777777777777}
	s.Sign(r)
	u, err := Encode("Receipt", &Object[receipt.Receipt, struct{}]{Meta: metav1.ObjectMeta{Name: "x"}, Spec: *r})
	if err != nil {
		t.Fatal(err)
	}
	back, err := Decode[receipt.Receipt, struct{}](u)
	if err != nil {
		t.Fatal(err)
	}
	if back.Spec.Seed != r.Seed {
		t.Fatalf("seed %d came back as %d", r.Seed, back.Spec.Seed)
	}
	if err := receipt.Verify(&back.Spec, s.PublicKey()); err != nil {
		t.Fatal(err)
	}
}
