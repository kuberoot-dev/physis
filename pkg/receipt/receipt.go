// Package receipt makes every result verifiable (principle P9). A receipt
// records who asked and under which mandate, what was computed and on what,
// what came out and what it cost. The agent that ran the work signs it with
// its Ed25519 key, and each receipt carries the hash of the agent's previous
// one, so an agent's history is a hash chain: removing or editing a receipt
// breaks every later link. Anyone with the agent's public key can check a
// chain offline, without access to the system that produced it.
package receipt

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// Receipt is the record of one run.
type Receipt struct {
	// Who asked.
	Problem   string `json:"problem"`   // namespace/name
	Requester string `json:"requester"` // identity that created the problem
	Mandate   string `json:"mandate,omitempty"`
	// What was computed.
	InstanceDigest string `json:"instanceDigest"`
	Algorithm      string `json:"algorithm"`
	Effort         string `json:"effort"`
	Code           string `json:"code"` // build of the agent
	Seed           uint64 `json:"seed"`
	// On what.
	Substrate string `json:"substrate"`
	Physics   string `json:"physics"`
	Emulated  bool   `json:"emulated"`
	Zone      string `json:"zone"`
	// What came out.
	ResultDigest string  `json:"resultDigest"`
	Quality      float64 `json:"quality"`
	Value        float64 `json:"value"` // cut weight or correct count
	// What it cost. Device figures are what the substrate model charges for
	// the counted operations; host figures are what the agent's process
	// actually consumed, which for an emulated substrate is the emulator.
	Started        time.Time `json:"started"`
	Finished       time.Time `json:"finished"`
	SpinUpdates    int64     `json:"spinUpdates"`
	MACs           int64     `json:"macs"`
	DeviceSeconds  float64   `json:"deviceSeconds"`
	DeviceJoules   float64   `json:"deviceJoules"`
	HostCPUSeconds float64   `json:"hostCpuSeconds"`
	EnergySource   string    `json:"energySource"` // "model" or "measured"
	CI             float64   `json:"ciGPerKWh"`
	GCO2           float64   `json:"gco2"`
	EUR            float64   `json:"eur"`
	// Chain.
	Seq  uint64 `json:"seq"`
	Prev string `json:"prev"` // hash of the previous receipt of this agent
	// Signature over the canonical body, base64; Key is the agent's public
	// key, hex.
	Key       string `json:"key"`
	Signature string `json:"signature,omitempty"`
}

// body is the canonical form that is hashed and signed: the receipt with an
// empty signature, marshalled by encoding/json (struct field order is
// fixed, so the bytes are deterministic).
func (r *Receipt) body() []byte {
	c := *r
	c.Signature = ""
	b, _ := json.Marshal(&c)
	return b
}

// Hash identifies a signed receipt; the next receipt's Prev points at it.
func (r *Receipt) Hash() string {
	sum := sha256.Sum256(append(r.body(), []byte(r.Signature)...))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// Signer is an agent's signing identity and the head of its chain.
type Signer struct {
	priv ed25519.PrivateKey
	seq  uint64
	head string
}

// NewSigner wraps a private key; seq and head resume an existing chain.
func NewSigner(priv ed25519.PrivateKey, seq uint64, head string) *Signer {
	return &Signer{priv: priv, seq: seq, head: head}
}

// PublicKey in hex.
func (s *Signer) PublicKey() string {
	return hex.EncodeToString(s.priv.Public().(ed25519.PublicKey))
}

// Head returns the sequence number and hash of the last signed receipt.
func (s *Signer) Head() (uint64, string) { return s.seq, s.head }

// Sign links the receipt to the chain and signs it.
func (s *Signer) Sign(r *Receipt) {
	s.seq++
	r.Seq = s.seq
	r.Prev = s.head
	r.Key = s.PublicKey()
	r.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(s.priv, r.body()))
	s.head = r.Hash()
}

// Verify checks one receipt's signature against its embedded key and, when
// trusted is non-empty, that the key is the trusted one.
func Verify(r *Receipt, trusted string) error {
	if trusted != "" && r.Key != trusted {
		return fmt.Errorf("receipt %d signed by %s, expected %s", r.Seq, short(r.Key), short(trusted))
	}
	pub, err := hex.DecodeString(r.Key)
	if err != nil || len(pub) != ed25519.PublicKeySize {
		return errors.New("malformed key")
	}
	sig, err := base64.StdEncoding.DecodeString(r.Signature)
	if err != nil {
		return errors.New("malformed signature")
	}
	if !ed25519.Verify(pub, r.body(), sig) {
		return fmt.Errorf("receipt %d: bad signature", r.Seq)
	}
	return nil
}

// VerifyChain checks signatures, sequence numbers and links of an agent's
// receipts in order. It reports the first break.
func VerifyChain(rs []*Receipt, trusted string) error {
	prev := ""
	for i, r := range rs {
		if err := Verify(r, trusted); err != nil {
			return err
		}
		if i > 0 {
			if r.Seq != rs[i-1].Seq+1 {
				return fmt.Errorf("receipt %d follows %d: a receipt is missing", r.Seq, rs[i-1].Seq)
			}
			if r.Prev != prev {
				return fmt.Errorf("receipt %d does not link to receipt %d", r.Seq, rs[i-1].Seq)
			}
		}
		prev = r.Hash()
	}
	return nil
}

func short(k string) string {
	if len(k) > 12 {
		return k[:12]
	}
	return k
}
