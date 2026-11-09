package identity

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	v1 "github.com/kuberoot-dev/physis/api/v1alpha1"
	"github.com/kuberoot-dev/physis/pkg/plan"
)

// agent is one in-process substrate: a key, a store, a direct endpoint.
type agent struct {
	key   ed25519.PrivateKey
	rec   Record
	store *MemStore
	srv   *Server
	ln    *Listener
}

func startAgent(t *testing.T, d *dir, key ed25519.PrivateKey, name, zone, jur, addr string) *agent {
	t.Helper()
	a := &agent{key: key, store: &MemStore{}}
	a.rec = Record{ID: Of(key), Substrate: name, Zone: zone, Jurisdiction: jur}
	a.srv = &Server{Key: key, Zone: zone, Store: a.store, Resolver: &Resolver{Dir: d}, Logf: t.Logf}
	if addr != "" {
		a.listen(t, d, addr)
	} else {
		d.set(a.rec)
	}
	t.Cleanup(func() {
		if a.ln != nil {
			a.ln.Close()
		}
	})
	return a
}

// listen starts the direct endpoint and publishes it, as the agent's
// heartbeat publishes status.endpoints.
func (a *agent) listen(t *testing.T, d *dir, addr string) {
	t.Helper()
	ln, err := a.srv.Start(addr)
	if err != nil {
		t.Fatal(err)
	}
	a.ln = ln
	a.rec.Endpoints = []v1.Endpoint{{Transport: v1.TransportHTTPS, Address: ln.Addr()}}
	d.set(a.rec)
}

func (a *agent) client(d *dir) *Client {
	return &Client{Key: a.key, Resolver: &Resolver{Dir: d}, Backoff: time.Millisecond, ChunkSize: 64 << 10}
}

func randomBytes(t *testing.T, n int) []byte {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return b
}

// TestFetchFollowsIdentityAcrossMove is the main scenario: agent A fetches
// a dataset from identity B. After the first chunk B stops, comes back on
// another port with the same key, and its old address is taken by another
// identity C serving an object of the same name. A must notice that the old
// address is no longer B (the TLS key does not match), resolve B again,
// and continue from where it stopped, ending with B's bytes, not C's.
func TestFetchFollowsIdentityAcrossMove(t *testing.T) {
	d := newDir()
	a := startAgent(t, d, newKey(t), "a", "DE", "EU", "")
	b := startAgent(t, d, newKey(t), "b", "DE", "EU", "127.0.0.1:0")
	data := randomBytes(t, 300<<10) // five chunks of 64 KiB
	b.store.Put("dataset", &Object{Data: data, Jurisdictions: []string{"EU"}})

	oldAddr := b.ln.Addr()
	var impostor *agent
	moved := false
	c := a.client(d)
	c.OnChunk = func(done, total int64) {
		if moved {
			return
		}
		moved = true
		b.ln.Close()
		// The old address now belongs to someone else.
		impostor = startAgent(t, d, newKey(t), "c", "DE", "EU", oldAddr)
		impostor.store.Put("dataset", &Object{Data: randomBytes(t, len(data))})
		// B restarts elsewhere and publishes its new endpoint.
		b.listen(t, d, "127.0.0.1:0")
	}
	f, err := c.Fetch(context.Background(), b.rec.ID, "dataset")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(f.Data, data) || f.Digest != Digest(data) {
		t.Fatal("fetched bytes are not B's")
	}
	if b.ln.Addr() == oldAddr || impostor == nil {
		t.Fatal("the move did not happen")
	}
	if f.Resolutions < 2 || f.Retries < 1 {
		t.Fatalf("expected a re-resolution after the move: %+v", struct{ Resolutions, Retries int }{f.Resolutions, f.Retries})
	}
	for _, e := range f.Failures {
		t.Logf("retried after: %v", e)
	}
	if f.Via != v1.TransportHTTPS {
		t.Fatalf("via %s", f.Via)
	}
}

// TestFetchRefusesWrongPeer: an endpoint that points at another identity
// never yields data, whatever that identity serves.
func TestFetchRefusesWrongPeer(t *testing.T) {
	d := newDir()
	a := startAgent(t, d, newKey(t), "a", "DE", "EU", "")
	c := startAgent(t, d, newKey(t), "c", "DE", "EU", "127.0.0.1:0")
	c.store.Put("dataset", &Object{Data: []byte("not B's")})
	// B is registered with C's address: stale, or forged by whoever can
	// write substrate status.
	bKey := newKey(t)
	d.set(Record{ID: Of(bKey), Substrate: "b", Zone: "DE", Jurisdiction: "EU",
		Endpoints: []v1.Endpoint{{Transport: v1.TransportHTTPS, Address: c.ln.Addr()}}})
	cl := a.client(d)
	cl.Attempts = 2
	_, err := cl.Fetch(context.Background(), Of(bKey), "dataset")
	if !errors.Is(err, ErrWrongPeer) {
		t.Fatalf("expected ErrWrongPeer, got %v", err)
	}
}

// TestServerAuthenticatesCaller: B decides by the caller's identity, never
// its address: an unregistered identity, a receiver outside the data's
// jurisdictions and a path through a forbidden jurisdiction are refused.
func TestServerAuthenticatesCaller(t *testing.T) {
	d := newDir()
	b := startAgent(t, d, newKey(t), "b", "FR", "EU", "127.0.0.1:0")
	b.store.Put("eu-only", &Object{Data: []byte("eu data"), Jurisdictions: []string{"EU"}})
	b.store.Put("open", &Object{Data: []byte("open data")})

	stranger := &Client{Key: newKey(t), Resolver: &Resolver{Dir: d}, Backoff: time.Millisecond}
	if _, err := stranger.Fetch(context.Background(), b.rec.ID, "open"); !errors.Is(err, ErrRefused) {
		t.Fatalf("unregistered caller: %v", err)
	}
	gb := startAgent(t, d, newKey(t), "gb", "GB", "UK", "")
	if _, err := gb.client(d).Fetch(context.Background(), b.rec.ID, "eu-only"); !errors.Is(err, ErrRefused) {
		t.Fatalf("UK receiver of EU-only data: %v", err)
	}
	if f, err := gb.client(d).Fetch(context.Background(), b.rec.ID, "open"); err != nil || string(f.Data) != "open data" {
		t.Fatalf("open data to UK: %v", err)
	}
	// An EU receiver whose only path from FR goes through the UK.
	ie := startAgent(t, d, newKey(t), "ie", "IE", "EU", "")
	var mu sync.Mutex
	var links []plan.Link
	var linksErr error
	setLinks := func(l []plan.Link, err error) { mu.Lock(); links, linksErr = l, err; mu.Unlock() }
	b.srv.Links = func(context.Context) ([]plan.Link, error) { mu.Lock(); defer mu.Unlock(); return links, linksErr }
	setLinks([]plan.Link{{Name: "fr-gb-ie", From: "FR", To: "IE", Jurisdictions: []string{"EU", "UK"}}}, nil)
	if _, err := ie.client(d).Fetch(context.Background(), b.rec.ID, "eu-only"); err == nil || !strings.Contains(err.Error(), "crosses jurisdiction UK") {
		t.Fatalf("path through UK: %v", err)
	}
	// Links that cannot be read refuse rather than allow.
	setLinks(nil, errors.New("api down"))
	if _, err := ie.client(d).Fetch(context.Background(), b.rec.ID, "open"); !errors.Is(err, ErrRefused) {
		t.Fatalf("unknown links: %v", err)
	}
	setLinks(nil, nil)
	if _, err := ie.client(d).Fetch(context.Background(), b.rec.ID, "missing"); !errors.Is(err, ErrNoObject) {
		t.Fatalf("missing object: %v", err)
	}
}

func TestFetchSmallAndEmptyObjects(t *testing.T) {
	d := newDir()
	a := startAgent(t, d, newKey(t), "a", "DE", "EU", "")
	b := startAgent(t, d, newKey(t), "b", "DE", "EU", "127.0.0.1:0")
	b.store.Put("empty", &Object{})
	b.store.Put("one", &Object{Data: []byte{42}})
	for name, want := range map[string][]byte{"empty": nil, "one": {42}} {
		f, err := a.client(d).Fetch(context.Background(), b.rec.ID, name)
		if err != nil || !bytes.Equal(f.Data, want) {
			t.Fatalf("%s: %v %v", name, f, err)
		}
	}
}

func TestFetchDetectsChangedObject(t *testing.T) {
	d := newDir()
	a := startAgent(t, d, newKey(t), "a", "DE", "EU", "")
	b := startAgent(t, d, newKey(t), "b", "DE", "EU", "127.0.0.1:0")
	b.store.Put("dataset", &Object{Data: randomBytes(t, 200<<10)})
	c := a.client(d)
	c.OnChunk = func(done, total int64) {
		if done < total {
			b.store.Put("dataset", &Object{Data: randomBytes(t, 200<<10)})
		}
	}
	if _, err := c.Fetch(context.Background(), b.rec.ID, "dataset"); err == nil || !strings.Contains(err.Error(), "changed") {
		t.Fatalf("expected a change to be detected, got %v", err)
	}
}

// serveRelay runs B's relay loop as its agent would.
func serveRelay(t *testing.T, s *Server, r Relay) {
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for ctx.Err() == nil {
			if _, err := s.ServeRelay(ctx, r); err != nil {
				t.Error(err)
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
	}()
	t.Cleanup(func() { cancel(); wg.Wait() })
}

// TestRelayFallback: B has no direct endpoint (behind NAT) and answers
// through the relay; the same identity checks and policy apply.
func TestRelayFallback(t *testing.T) {
	d := newDir()
	relay := &MemRelay{}
	a := startAgent(t, d, newKey(t), "a", "DE", "EU", "")
	gb := startAgent(t, d, newKey(t), "gb", "GB", "UK", "")
	b := startAgent(t, d, newKey(t), "b", "DE", "EU", "")
	b.rec.Endpoints = []v1.Endpoint{{Transport: v1.TransportRelay}}
	d.set(b.rec)
	small := randomBytes(t, 10<<10)
	b.store.Put("small", &Object{Data: small, Jurisdictions: []string{"EU"}})
	b.store.Put("large", &Object{Data: randomBytes(t, MaxRelayBytes+1)})
	serveRelay(t, b.srv, relay)

	c := a.client(d)
	c.Relay = relay
	f, err := c.Fetch(context.Background(), b.rec.ID, "small")
	if err != nil || !bytes.Equal(f.Data, small) || f.Via != v1.TransportRelay {
		t.Fatalf("relay fetch: %v", err)
	}
	if _, err := c.Fetch(context.Background(), b.rec.ID, "large"); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("large object through the relay: %v", err)
	}
	g := gb.client(d)
	g.Relay = relay
	if _, err := g.Fetch(context.Background(), b.rec.ID, "small"); !errors.Is(err, ErrRefused) {
		t.Fatalf("UK receiver through the relay: %v", err)
	}
	// Without a relay the identity is unreachable, and says so.
	if _, err := a.client(d).Fetch(context.Background(), b.rec.ID, "small"); !errors.Is(err, ErrNoEndpoint) {
		t.Fatalf("no relay: %v", err)
	}
}

// TestRelayRejectsForgery: anyone can post to the mailbox, so a forged
// answer in B's name and a forged request in A's name are both ignored.
func TestRelayRejectsForgery(t *testing.T) {
	d := newDir()
	relay := &MemRelay{}
	a := startAgent(t, d, newKey(t), "a", "DE", "EU", "")
	b := startAgent(t, d, newKey(t), "b", "DE", "EU", "")
	b.rec.Endpoints = []v1.Endpoint{{Transport: v1.TransportRelay}}
	d.set(b.rec)
	b.store.Put("x", &Object{Data: []byte("b's data")})
	forger := newKey(t)

	// The forger answers every request for B with its own data, signed with
	// its own key but claiming to be B.
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for ctx.Err() == nil {
			reqs, _ := relay.Take(ctx, b.rec.ID, KindRequest, nil)
			for _, q := range reqs {
				ans := &Envelope{Kind: KindResponse, From: b.rec.ID, To: q.From, Name: q.Name, Nonce: q.Nonce,
					Data: []byte("forged"), Digest: Digest([]byte("forged"))}
				ans.Sig = sign(forger, "relay", ans.parts()...)
				relay.Post(ctx, ans)
			}
			time.Sleep(time.Millisecond)
		}
	}()
	c := a.client(d)
	c.Relay, c.RelayWait = relay, 200*time.Millisecond
	if f, err := c.Fetch(context.Background(), b.rec.ID, "x"); err == nil {
		t.Fatalf("accepted a forged answer: %q", f.Data)
	}
	cancel()
	wg.Wait()

	// A request in A's name signed by the forger is dropped by B.
	req := &Envelope{Kind: KindRequest, From: a.rec.ID, To: b.rec.ID, Name: "x"}
	req.Sig = sign(forger, "relay", req.parts()...)
	relay.Post(context.Background(), req)
	n, err := b.srv.ServeRelay(context.Background(), relay)
	if err != nil || n != 0 {
		t.Fatalf("forged request answered: %d, %v", n, err)
	}
	if ans, _ := relay.Take(context.Background(), a.rec.ID, KindResponse, nil); len(ans) != 0 {
		t.Fatal("an answer was posted for a forged request")
	}
}
