package receipt

import (
	"fmt"
	"sort"
)

// ChainReport is the verdict on the receipts one substrate signed with one
// key.
type ChainReport struct {
	Substrate string `json:"substrate"`
	Key       string `json:"key"`
	// Published: the key is one the substrate lists, current or retired.
	Published bool   `json:"published"`
	FirstSeq  uint64 `json:"firstSeq"`
	LastSeq   uint64 `json:"lastSeq"`
	// Verified receipts form the contiguous tail of the chain; Skipped ones
	// precede a gap (receipts deleted with their namespace, or kept where
	// the caller cannot read) and are not judged.
	Verified int    `json:"verified"`
	Skipped  int    `json:"skipped,omitempty"`
	Error    string `json:"error,omitempty"`
	// Tail is the verified receipts, in order.
	Tail []*Receipt `json:"-"`
}

// OK reports whether the chain verified.
func (c ChainReport) OK() bool { return c.Error == "" }

// CheckChains verifies receipts offline, the way anyone holding them and the
// substrates' published keys can. Receipts are grouped by substrate and
// signing key; a key the substrate never published (keys maps a substrate
// to its current and retired keys) fails the chain, and so does a broken
// signature, sequence or link in the contiguous tail. Reports are ordered by
// substrate, then key.
func CheckChains(rs []*Receipt, keys map[string][]string) []ChainReport {
	type id struct{ sub, key string }
	chains := map[id][]*Receipt{}
	for _, r := range rs {
		k := id{r.Substrate, r.Key}
		chains[k] = append(chains[k], r)
	}
	ids := make([]id, 0, len(chains))
	for k := range chains {
		ids = append(ids, k)
	}
	sort.Slice(ids, func(i, j int) bool {
		if ids[i].sub != ids[j].sub {
			return ids[i].sub < ids[j].sub
		}
		return ids[i].key < ids[j].key
	})
	var out []ChainReport
	for _, k := range ids {
		c := append([]*Receipt(nil), chains[k]...)
		sort.Slice(c, func(i, j int) bool { return c[i].Seq < c[j].Seq })
		start := 0
		for i := 1; i < len(c); i++ {
			if c[i].Seq != c[i-1].Seq+1 {
				start = i
			}
		}
		tail := c[start:]
		rep := ChainReport{Substrate: k.sub, Key: k.key, FirstSeq: tail[0].Seq, LastSeq: tail[len(tail)-1].Seq, Skipped: start}
		for _, pk := range keys[k.sub] {
			if pk != "" && pk == k.key {
				rep.Published = true
			}
		}
		switch {
		case !rep.Published:
			rep.Error = fmt.Sprintf("signed with %s, a key the substrate never published", short(k.key))
		default:
			if err := VerifyChain(tail, k.key); err != nil {
				rep.Error = err.Error()
			} else {
				rep.Verified, rep.Tail = len(tail), tail
			}
		}
		out = append(out, rep)
	}
	return out
}
