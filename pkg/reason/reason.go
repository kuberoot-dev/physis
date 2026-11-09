// Package reason defines the stable, machine-readable codes physis gives for
// refusing work or rejecting a way to run it (principle P14). The free-text
// reasons stay for people; an agent branches on the codes, which do not change
// when the wording does. A code names a cause, not a component: the same code
// is used whether the scheduler, the planner or a mandate found it.
package reason

// Codes. New codes may be added; existing ones are never renamed or reused.
const (
	// ZoneNotAllowed: the substrate's zone is outside the zones the problem
	// (or its mandate) allows.
	ZoneNotAllowed = "ZoneNotAllowed"
	// JurisdictionNotAllowed: the operator's jurisdiction, or one a data
	// path crosses, is outside the jurisdictions the problem allows.
	JurisdictionNotAllowed = "JurisdictionNotAllowed"
	// BudgetExceeded: the predicted energy, money or carbon is over the
	// problem's budget.
	BudgetExceeded = "BudgetExceeded"
	// QualityUnreachable: the learned lower bound on reaching the quality
	// target is below the requested confidence.
	QualityUnreachable = "QualityUnreachable"
	// DeadlineTooShort: the run would finish after the deadline.
	DeadlineTooShort = "DeadlineTooShort"
	// NoLink: the problem's data cannot reach the substrate's zone.
	NoLink = "NoLink"
	// GridUnavailable: no grid forecast for the substrate's zone, so its
	// carbon and price cannot be predicted.
	GridUnavailable = "GridUnavailable"
	// NoSubstrates: no live substrate could take the work at all.
	NoSubstrates = "NoSubstrates"
	// InvalidRequest: the problem itself cannot be resolved (unknown
	// instance, class or shape).
	InvalidRequest = "InvalidRequest"
	// MandateInvalid: the named mandate is missing, not signed by a trusted
	// person's key, or covers another namespace.
	MandateInvalid = "MandateInvalid"
	// MandateDenied: the mandate is valid but does not admit this plan
	// (expired, zone not delegated, daily budget spent).
	MandateDenied = "MandateDenied"
	// Duplicate: another problem in the namespace carries the same
	// spec.requestID; status.duplicateOf names it.
	Duplicate = "Duplicate"
	// RateLimited: the namespace submitted more new problems than the
	// scheduler admits per window; retry later, under a new name.
	RateLimited = "RateLimited"
)

// Descriptions explains every code in one line, for tool schemas and docs.
var Descriptions = map[string]string{
	ZoneNotAllowed:         "the zone is outside the zones the problem or its mandate allows",
	JurisdictionNotAllowed: "the operator's jurisdiction or a data path's is outside the allowed jurisdictions",
	BudgetExceeded:         "predicted joules, euros or grams of CO2e exceed the budget",
	QualityUnreachable:     "the quality target cannot be reached with the requested confidence",
	DeadlineTooShort:       "the run would finish after the deadline",
	NoLink:                 "the problem's data has no allowed path to the substrate's zone",
	GridUnavailable:        "no grid forecast for the zone",
	NoSubstrates:           "no live substrate could take the work",
	InvalidRequest:         "the problem cannot be resolved (instance, class or shape)",
	MandateInvalid:         "the mandate is missing, wrongly signed or covers another namespace",
	MandateDenied:          "the mandate is valid but does not admit this plan",
	Duplicate:              "another problem in the namespace has the same requestID (see status.duplicateOf)",
	RateLimited:            "too many new problems from this namespace; retry later",
}

// All lists the codes in a stable order.
func All() []string {
	return []string{ZoneNotAllowed, JurisdictionNotAllowed, BudgetExceeded, QualityUnreachable, DeadlineTooShort,
		NoLink, GridUnavailable, NoSubstrates, InvalidRequest, MandateInvalid, MandateDenied, Duplicate, RateLimited}
}

// Add appends code to codes unless it is already there, keeping the order in
// which codes were first seen.
func Add(codes []string, code string) []string {
	for _, c := range codes {
		if c == code {
			return codes
		}
	}
	return append(codes, code)
}
