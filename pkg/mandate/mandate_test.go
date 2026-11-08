package mandate

import (
	"crypto/ed25519"
	"crypto/rand"
	"testing"
	"time"
)

var now = time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)

func signed(t *testing.T, m Mandate) (*Mandate, map[string]string) {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	m.Sign(priv)
	return &m, map[string]string{m.Principal: m.Key}
}

func base() Mandate {
	return Mandate{Principal: "anna", Namespace: "team-a", Zones: []string{"FR", "DE"},
		NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour), JoulesPerDay: 1e6, Autonomy: Directed}
}

func TestSignedMandateVerifiesAndTamperingDoesNot(t *testing.T) {
	m, trust := signed(t, base())
	if err := m.Verify(trust); err != nil {
		t.Fatal(err)
	}
	m.JoulesPerDay = 1e12 // an agent raising its own budget
	if err := m.Verify(trust); err == nil {
		t.Fatal("tampered mandate verified")
	}
}

func TestUntrustedPrincipalIsRefused(t *testing.T) {
	m, _ := signed(t, base())
	if err := m.Verify(map[string]string{}); err == nil {
		t.Fatal("mandate of an unknown principal accepted")
	}
}

func TestAdmission(t *testing.T) {
	m, _ := signed(t, base())
	if v := m.Admit(now, "PL", 10, 0, 0, Spent{}); v.Allowed {
		t.Fatal("undelegated zone admitted")
	}
	if v := m.Admit(now, "FR", 10, 0, 0, Spent{Joules: 1e6}); v.Allowed {
		t.Fatal("exhausted budget admitted")
	}
	if v := m.Admit(now.Add(2*time.Hour), "FR", 10, 0, 0, Spent{}); v.Allowed {
		t.Fatal("expired mandate admitted")
	}
	if v := m.Admit(now, "FR", 10, 0, 0, Spent{}); !v.Allowed || v.NeedsApproval {
		t.Fatalf("valid plan refused: %+v", v)
	}
	a := base()
	a.Autonomy, a.ApproveAbove = Augmented, 100
	am, _ := signed(t, a)
	if v := am.Admit(now, "FR", 500, 0, 0, Spent{}); !v.NeedsApproval {
		t.Fatal("large plan under augmented autonomy ran without approval")
	}
	if v := am.Admit(now, "FR", 50, 0, 0, Spent{}); v.NeedsApproval {
		t.Fatal("small plan under augmented autonomy waited")
	}
}
