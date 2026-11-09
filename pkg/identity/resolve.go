package identity

import (
	"context"
	"errors"
	"fmt"
	"slices"

	v1 "github.com/kuberoot-dev/physis/api/v1alpha1"
)

// Record is what the API says about an identity: the substrate that holds
// it, where it is and under which jurisdiction, and how to reach it now.
type Record struct {
	ID           ID
	Substrate    string
	Zone         string
	Jurisdiction string
	Endpoints    []v1.Endpoint
}

// Endpoint returns the first endpoint of a transport.
func (r Record) Endpoint(transport string) (v1.Endpoint, bool) {
	for _, e := range r.Endpoints {
		if e.Transport == transport {
			return e, true
		}
	}
	return v1.Endpoint{}, false
}

// RecordOf reads a substrate's record; ok is false when it has no valid key.
// Retired keys do not resolve: they verify old receipts and reach nothing.
func RecordOf(name string, spec v1.SubstrateSpec, status v1.SubstrateStatus) (Record, bool) {
	id, err := FromHex(status.PublicKey)
	if err != nil {
		return Record{}, false
	}
	return Record{ID: id, Substrate: name, Zone: spec.Zone, Jurisdiction: spec.Jurisdiction, Endpoints: status.Endpoints}, true
}

// Directory lists the current records, normally from the Substrate objects.
type Directory interface {
	Records(ctx context.Context) ([]Record, error)
}

// DirectoryFunc adapts a function to a Directory.
type DirectoryFunc func(ctx context.Context) ([]Record, error)

func (f DirectoryFunc) Records(ctx context.Context) ([]Record, error) { return f(ctx) }

// ErrUnknown is returned for an identity no substrate holds.
var ErrUnknown = errors.New("identity: not held by any substrate")

// Resolver maps identities to records. It caches nothing: the agents of a
// prototype are few, and a stale answer is exactly what re-resolution after
// a move must avoid.
type Resolver struct {
	Dir Directory
}

// Resolve finds the record of an identity. Two substrates claiming one key
// is an error, not a choice: the jurisdiction of the identity would be
// ambiguous, and copying someone's public key must not redirect anything.
func (r *Resolver) Resolve(ctx context.Context, id ID) (Record, error) {
	recs, err := r.Dir.Records(ctx)
	if err != nil {
		return Record{}, fmt.Errorf("identity: resolve %s: %w", id.Short(), err)
	}
	var found []Record
	for _, rec := range recs {
		if rec.ID == id {
			found = append(found, rec)
		}
	}
	switch len(found) {
	case 0:
		return Record{}, fmt.Errorf("%w: %s", ErrUnknown, id.Short())
	case 1:
		return found[0], nil
	default:
		names := make([]string, len(found))
		for i, f := range found {
			names[i] = f.Substrate
		}
		slices.Sort(names)
		return Record{}, fmt.Errorf("identity: %s is claimed by substrates %v", id.Short(), names)
	}
}
