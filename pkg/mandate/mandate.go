// Package mandate implements principle P10: people set limits, agents act
// within them. A mandate is a statement signed by a person's Ed25519 key that
// delegates a scope (a namespace), a budget per day, the zones work may run
// in and a validity period. The control plane refuses a problem that no valid
// mandate covers, and stops admitting work once the day's budget is spent.
package mandate

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// Autonomy is how much the person delegated. The four levels follow the
// "four cultures of AI" of IFTF's 2036 forecast: no AI, AI-augmented,
// AI-adjacent, AI-directed.
type Autonomy string

const (
	// Manual: every plan waits for a person's approval.
	Manual Autonomy = "manual"
	// Augmented: plans under ApproveAbove run; larger ones wait.
	Augmented Autonomy = "augmented"
	// Directed: agents act alone within the budget.
	Directed Autonomy = "directed"
)

// Mandate is the signed delegation.
type Mandate struct {
	Principal string    `json:"principal"` // who signs: a person, not an agent
	Namespace string    `json:"namespace"`
	Zones     []string  `json:"zones,omitempty"`
	NotBefore time.Time `json:"notBefore"`
	NotAfter  time.Time `json:"notAfter"`
	// Daily budget; zero means no cap on that quantity.
	JoulesPerDay float64  `json:"joulesPerDay,omitempty"`
	EURPerDay    float64  `json:"eurPerDay,omitempty"`
	GCO2PerDay   float64  `json:"gco2PerDay,omitempty"`
	Autonomy     Autonomy `json:"autonomy"`
	// ApproveAbove: with Augmented autonomy, plans predicted to cost more
	// joules than this wait for approval.
	ApproveAbove float64 `json:"approveAboveJoules,omitempty"`
	Key          string  `json:"key"`
	Signature    string  `json:"signature,omitempty"`
}

func (m *Mandate) body() []byte {
	c := *m
	c.Signature = ""
	b, _ := json.Marshal(&c)
	return b
}

// Sign signs the mandate with the principal's key.
func (m *Mandate) Sign(priv ed25519.PrivateKey) {
	m.Key = hex.EncodeToString(priv.Public().(ed25519.PublicKey))
	m.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(priv, m.body()))
}

// Verify checks the signature and that the key belongs to a trusted
// principal (a person registered with the control plane).
func (m *Mandate) Verify(trustedKeys map[string]string) error {
	want, ok := trustedKeys[m.Principal]
	if !ok {
		return fmt.Errorf("principal %q is not trusted", m.Principal)
	}
	if want != m.Key {
		return fmt.Errorf("mandate of %q signed by an unknown key", m.Principal)
	}
	pub, err := hex.DecodeString(m.Key)
	if err != nil || len(pub) != ed25519.PublicKeySize {
		return errors.New("malformed key")
	}
	sig, err := base64.StdEncoding.DecodeString(m.Signature)
	if err != nil || !ed25519.Verify(pub, m.body(), sig) {
		return errors.New("bad signature")
	}
	return nil
}

// Spent is what a namespace consumed today.
type Spent struct {
	Joules, EUR, GCO2 float64
}

// Verdict of an admission check.
type Verdict struct {
	Allowed       bool
	NeedsApproval bool
	Reason        string
}

// Admit decides whether a plan with the given predicted cost may run now.
func (m *Mandate) Admit(now time.Time, zone string, joules, eur, gco2 float64, spent Spent) Verdict {
	if now.Before(m.NotBefore) || now.After(m.NotAfter) {
		return Verdict{Reason: "mandate not valid at this time"}
	}
	if len(m.Zones) > 0 {
		ok := false
		for _, z := range m.Zones {
			if z == zone {
				ok = true
			}
		}
		if !ok {
			return Verdict{Reason: fmt.Sprintf("zone %s not delegated", zone)}
		}
	}
	if m.JoulesPerDay > 0 && spent.Joules+joules > m.JoulesPerDay {
		return Verdict{Reason: fmt.Sprintf("daily energy budget: %.0f J spent, %.0f J asked, %.0f J delegated", spent.Joules, joules, m.JoulesPerDay)}
	}
	if m.EURPerDay > 0 && spent.EUR+eur > m.EURPerDay {
		return Verdict{Reason: "daily money budget exhausted"}
	}
	if m.GCO2PerDay > 0 && spent.GCO2+gco2 > m.GCO2PerDay {
		return Verdict{Reason: "daily carbon budget exhausted"}
	}
	switch m.Autonomy {
	case Manual:
		return Verdict{Allowed: true, NeedsApproval: true, Reason: "manual mandate: every plan needs approval"}
	case Augmented:
		if joules > m.ApproveAbove {
			return Verdict{Allowed: true, NeedsApproval: true, Reason: fmt.Sprintf("plan of %.0f J above the %.0f J approval threshold", joules, m.ApproveAbove)}
		}
	}
	return Verdict{Allowed: true}
}
