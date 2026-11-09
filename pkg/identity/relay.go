package identity

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"
)

// The relay is the fallback for an identity with no direct endpoint, such
// as an agent behind NAT that can reach the API server but accepts no
// connections. Both sides exchange small signed messages through a shared
// mailbox. Every message is signed by its sender's identity key, the answer
// carries the caller's nonce, so the answer is a challenge-response: only
// the holder of the callee's key can produce it, and it cannot be replayed
// to another request. The mailbox, and whoever can read it, sees the data:
// messages are signed, not encrypted.
//
// The package defines the mailbox as an interface and implements it in
// memory. Backing it with API objects (one small object per message,
// deleted on take) is the step that remains; the size limit below is set
// for that, well under what the API server stores in one object.

// MaxRelayBytes is the most data one relayed answer may carry.
const MaxRelayBytes = 256 << 10

// Kinds of relay messages.
const (
	KindRequest  = "request"
	KindResponse = "response"
)

// Envelope is one relayed message.
type Envelope struct {
	Kind   string `json:"kind"`
	From   ID     `json:"from"`
	To     ID     `json:"to"`
	Name   string `json:"name"`
	Nonce  Nonce  `json:"nonce"`
	Digest string `json:"digest,omitempty"`
	Data   []byte `json:"data,omitempty"`
	Error  string `json:"error,omitempty"`
	Sig    []byte `json:"sig"`
}

func (e *Envelope) parts() [][]byte {
	h := sha256.Sum256(e.Data)
	return [][]byte{[]byte(e.Kind), e.From[:], e.To[:], []byte(e.Name), e.Nonce[:], []byte(e.Digest), []byte(e.Error), h[:]}
}

// Verify checks that the envelope was signed by the identity in From.
func (e *Envelope) Verify() error {
	if !verify(e.From, "relay", e.Sig, e.parts()...) {
		return fmt.Errorf("%w: relay message claims %s", ErrNotProven, e.From.Short())
	}
	return nil
}

// Relay is a mailbox between identities. Post must refuse data larger than
// MaxRelayBytes; Take removes and returns the messages of a kind addressed
// to an identity, only those with the given nonce when one is set.
type Relay interface {
	Post(ctx context.Context, e *Envelope) error
	Take(ctx context.Context, to ID, kind string, nonce *Nonce) ([]*Envelope, error)
}

// ErrTooLarge is returned for data over MaxRelayBytes.
var ErrTooLarge = errors.New("identity: too large for the relay")

// MemRelay is a Relay in memory.
type MemRelay struct {
	mu  sync.Mutex
	box []*Envelope
}

func (r *MemRelay) Post(_ context.Context, e *Envelope) error {
	if len(e.Data) > MaxRelayBytes {
		return fmt.Errorf("%w: %d bytes, limit %d", ErrTooLarge, len(e.Data), MaxRelayBytes)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	c := *e
	r.box = append(r.box, &c)
	return nil
}

func (r *MemRelay) Take(_ context.Context, to ID, kind string, nonce *Nonce) ([]*Envelope, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []*Envelope
	r.box = slices.DeleteFunc(r.box, func(e *Envelope) bool {
		if e.To == to && e.Kind == kind && (nonce == nil || e.Nonce == *nonce) {
			out = append(out, e)
			return true
		}
		return false
	})
	return out, nil
}

// viaRelay asks for an object through the relay and waits for the answer.
func (c *Client) viaRelay(ctx context.Context, from ID, name string) ([]byte, error) {
	n, err := NewNonce()
	if err != nil {
		return nil, err
	}
	self := Of(c.Key)
	req := &Envelope{Kind: KindRequest, From: self, To: from, Name: name, Nonce: n}
	req.Sig = sign(c.Key, "relay", req.parts()...)
	if err := c.Relay.Post(ctx, req); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, c.RelayWait)
	defer cancel()
	for {
		answers, err := c.Relay.Take(ctx, self, KindResponse, &n)
		if err != nil {
			return nil, err
		}
		for _, a := range answers {
			// Anyone can post to the mailbox; only an answer signed by the
			// identity asked, for this nonce, counts.
			if a.From != from || a.Name != name || a.Verify() != nil {
				continue
			}
			if a.Error != "" {
				return nil, remoteError(a.Error)
			}
			if Digest(a.Data) != a.Digest {
				return nil, fmt.Errorf("identity: relayed %s does not match its digest", name)
			}
			return a.Data, nil
		}
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("identity: no relayed answer from %s for %s: %w", from.Short(), name, ctx.Err())
		case <-time.After(50 * time.Millisecond):
		}
	}
}

// remoteError restores the sentinel of a peer's refusal carried as text.
func remoteError(msg string) error {
	for _, e := range []error{ErrTooLarge, ErrNoObject, ErrRefused} {
		if strings.HasPrefix(msg, e.Error()) {
			return fmt.Errorf("%w%s", e, strings.TrimPrefix(msg, e.Error()))
		}
	}
	return errors.New(msg)
}

// ServeRelay answers the requests waiting in the relay for this server's
// identity, under the same policy as the direct channel, and returns how
// many it answered. An agent calls it periodically.
func (s *Server) ServeRelay(ctx context.Context, r Relay) (int, error) {
	self := Of(s.Key)
	reqs, err := r.Take(ctx, self, KindRequest, nil)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, q := range reqs {
		if q.Verify() != nil {
			s.logf("identity: dropped a relay request with a bad signature claiming %s", q.From.Short())
			continue
		}
		a := &Envelope{Kind: KindResponse, From: self, To: q.From, Name: q.Name, Nonce: q.Nonce}
		o, _, err := s.authorize(ctx, q.From, q.Name)
		switch {
		case err != nil:
			a.Error = err.Error()
		case len(o.Data) > MaxRelayBytes:
			a.Error = fmt.Sprintf("%v: %d bytes, limit %d", ErrTooLarge, len(o.Data), MaxRelayBytes)
		default:
			a.Data, a.Digest = o.Data, o.Digest()
		}
		if err != nil {
			s.logf("identity: %s asked via relay for %s: %v", q.From.Short(), q.Name, err)
		}
		a.Sig = sign(s.Key, "relay", a.parts()...)
		if err := r.Post(ctx, a); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}
