package identity

import (
	"errors"
	"fmt"
	"slices"

	"github.com/kuberoot-dev/physis/pkg/plan"
)

// ErrRefused is returned when policy forbids sending to an identity.
var ErrRefused = errors.New("identity: refused by policy")

// AllowSend decides whether data allowed in the given jurisdictions may go
// from one substrate to another. It is the planner's rule for data paths
// (package plan) applied to an authenticated receiver instead of a planned
// one: the receiver's operator jurisdiction must be allowed, and when links
// are declared, a link must connect the two zones and its path must cross
// only allowed jurisdictions. Empty allowed means any jurisdiction; no
// declared links means the paths are not described and only the receiver
// is checked, as in the planner.
//
// The jurisdiction is what the receiver's substrate declares in the API.
// Authentication proves who the receiver is, not where it is: the claim is
// as good as the operator who registered the substrate.
func AllowSend(allowed []string, links []plan.Link, from, to Record) error {
	if len(allowed) > 0 && !slices.Contains(allowed, to.Jurisdiction) {
		return fmt.Errorf("%w: %s (substrate %s) is under jurisdiction %q, the data allows %v", ErrRefused, to.ID.Short(), to.Substrate, to.Jurisdiction, allowed)
	}
	l, ok := plan.FindLink(links, from.Zone, to.Zone)
	if !ok {
		if len(links) > 0 && from.Zone != to.Zone {
			return fmt.Errorf("%w: no link from %s to %s", ErrRefused, from.Zone, to.Zone)
		}
		return nil
	}
	if bad := l.Forbidden(allowed); bad != "" {
		return fmt.Errorf("%w: the path %s crosses jurisdiction %s", ErrRefused, l.Name, bad)
	}
	return nil
}
