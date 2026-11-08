// Package v1alpha1 defines the physis.kuberoot.dev API. The resources are
// served by an ordinary kube-apiserver as CRDs (principle P1: keep the
// contract) and are designed for machine callers first (P14).
package v1alpha1

import (
	"time"

	"github.com/kuberoot-dev/physis/pkg/plan"
	"github.com/kuberoot-dev/physis/pkg/receipt"
)

const (
	Group   = "physis.kuberoot.dev"
	Version = "v1alpha1"

	// AnnotationApproved marks a plan approved by a person when the mandate
	// requires approval.
	AnnotationApproved = Group + "/approved"
)

// SubstrateSpec describes a physical computing substrate. It replaces Node:
// a substrate need not be a computer with an operating system.
type SubstrateSpec struct {
	Physics      string `json:"physics"`
	Zone         string `json:"zone"`
	Operator     string `json:"operator"`
	Jurisdiction string `json:"jurisdiction"`
	Emulated     bool   `json:"emulated"`
	Slots        int    `json:"slots"`
	// Model parameters with provenance, for people and for any scheduler.
	Model []Parameter `json:"model"`
}

// Parameter is one model value and where it comes from.
type Parameter struct {
	Name   string  `json:"name"`
	Value  float64 `json:"value"`
	Lo     float64 `json:"lo,omitempty"`
	Hi     float64 `json:"hi,omitempty"`
	Source string  `json:"source"`
	Note   string  `json:"note,omitempty"`
}

// SubstrateStatus is written by the substrate's agent.
type SubstrateStatus struct {
	Agent     string    `json:"agent,omitempty"`
	Heartbeat time.Time `json:"heartbeat,omitempty"`
	PublicKey string    `json:"publicKey,omitempty"`
	// RetiredKeys are keys this substrate signed with before PublicKey:
	// receipts signed with them still verify, and a key change is recorded
	// rather than silent.
	RetiredKeys []string  `json:"retiredKeys,omitempty"`
	ChainSeq    uint64    `json:"chainSeq,omitempty"`
	ChainHead   string    `json:"chainHead,omitempty"`
	BusyUntil   time.Time `json:"busyUntil,omitempty"`
	Running     int       `json:"running,omitempty"`
}

// InstanceRef names the input of a problem. Exactly one field is set.
type InstanceRef struct {
	Gset   string       `json:"gset,omitempty"`
	Random *RandomGraph `json:"random,omitempty"`
	Digits *DigitsBatch `json:"digits,omitempty"`
}

type RandomGraph struct {
	N    int     `json:"n"`
	P    float64 `json:"p"`
	Seed uint64  `json:"seed"`
}

type DigitsBatch struct {
	Size   int `json:"size"`
	Offset int `json:"offset"`
}

// ProblemSpec is work stated as an outcome (P2).
type ProblemSpec struct {
	Class      string      `json:"class"`
	Instance   InstanceRef `json:"instance"`
	Quality    float64     `json:"quality"`
	Confidence float64     `json:"confidence,omitempty"`
	// Deadline is absolute; DeadlineSeconds is relative to creation.
	Deadline        *time.Time  `json:"deadline,omitempty"`
	DeadlineSeconds int64       `json:"deadlineSeconds,omitempty"`
	Deferrable      bool        `json:"deferrable,omitempty"`
	Policy          string      `json:"policy"`
	Budget          plan.Budget `json:"budget,omitempty"`
	Zones           []string    `json:"zones,omitempty"`
	Jurisdictions   []string    `json:"jurisdictions,omitempty"`
	DataZone        string      `json:"dataZone,omitempty"`
	DataBytes       float64     `json:"dataBytes,omitempty"`
	Mandate         string      `json:"mandate,omitempty"`
	// Physics restricts the substrates to these physics (empty = any).
	Physics []string `json:"physics,omitempty"`
	// DryRun asks for a plan and its alternatives without running (P14).
	DryRun bool `json:"dryRun,omitempty"`
}

// Phases of a problem.
const (
	PhasePending          = "Pending"
	PhasePlanned          = "Planned"
	PhaseAwaitingApproval = "AwaitingApproval"
	PhaseRunning          = "Running"
	PhaseSucceeded        = "Succeeded"
	PhaseRejected         = "Rejected"
	PhaseFailed           = "Failed"
)

// ProblemStatus carries the plan, the claim and the result.
type ProblemStatus struct {
	Phase      string           `json:"phase,omitempty"`
	Plan       *plan.Candidate  `json:"plan,omitempty"`
	Pareto     []plan.Candidate `json:"pareto,omitempty"`
	Candidates int              `json:"candidates,omitempty"`
	// FeasiblePhysics: every physics on which the outcome is reachable.
	FeasiblePhysics []string   `json:"feasiblePhysics,omitempty"`
	Reasons         []string   `json:"reasons,omitempty"`
	PlannedAt       *time.Time `json:"plannedAt,omitempty"`
	Replans         int        `json:"replans,omitempty"`
	// Claim: the substrate took the work. Replaces binding a pod to a node.
	ClaimedBy string     `json:"claimedBy,omitempty"`
	ClaimedAt *time.Time `json:"claimedAt,omitempty"`
	Result    *Result    `json:"result,omitempty"`
	Message   string     `json:"message,omitempty"`
}

// Result of a finished problem.
type Result struct {
	Quality      float64 `json:"quality"`
	Value        float64 `json:"value"`
	ResultDigest string  `json:"resultDigest"`
	Receipt      string  `json:"receipt"`
	DeviceJoules float64 `json:"deviceJoules"`
	GCO2         float64 `json:"gco2"`
}

// ReceiptSpec is the signed record (P9); see package receipt.
type ReceiptSpec = receipt.Receipt

// LinkSpec is a network path between zones as an object with a price
// (design section 4.4): energy per byte, latency, bandwidth, and the
// jurisdictions the path crosses.
type LinkSpec struct {
	From              string   `json:"from"`
	To                string   `json:"to"`
	JPerByte          float64  `json:"jPerByte"`
	LatencySeconds    float64  `json:"latencySeconds,omitempty"`
	BytesPerSecond    float64  `json:"bytesPerSecond,omitempty"`
	PathJurisdictions []string `json:"pathJurisdictions,omitempty"`
	Source            string   `json:"source"`
}
