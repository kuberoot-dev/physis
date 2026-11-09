package identity

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/kuberoot-dev/physis/pkg/plan"
)

// Object is a named piece of data a substrate holds, with the
// jurisdictions it may be sent to (normally those of the problem it
// belongs to; empty = any).
type Object struct {
	Data          []byte
	Jurisdictions []string
}

// Digest is the content address of the data.
func (o *Object) Digest() string { return Digest(o.Data) }

// Digest returns sha256:<hex> of b.
func Digest(b []byte) string {
	s := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(s[:])
}

// ErrNoObject is returned for a name the store does not hold.
var ErrNoObject = errors.New("identity: no such object")

// Store holds the objects a substrate serves.
type Store interface {
	Get(name string) (*Object, error)
}

// MemStore is a Store in memory.
type MemStore struct {
	mu sync.Mutex
	m  map[string]*Object
}

// Put adds or replaces an object.
func (s *MemStore) Put(name string, o *Object) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.m == nil {
		s.m = map[string]*Object{}
	}
	s.m[name] = o
}

func (s *MemStore) Get(name string) (*Object, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if o, ok := s.m[name]; ok {
		return o, nil
	}
	return nil, ErrNoObject
}

// DirStore serves the regular files of one directory, every one under the
// same jurisdictions. Names are file names; paths are refused.
type DirStore struct {
	Dir           string
	Jurisdictions []string
}

func (s DirStore) Get(name string) (*Object, error) {
	if !validName(name) {
		return nil, ErrNoObject
	}
	b, err := os.ReadFile(filepath.Join(s.Dir, name))
	if err != nil {
		return nil, ErrNoObject
	}
	return &Object{Data: b, Jurisdictions: s.Jurisdictions}, nil
}

func validName(name string) bool {
	return name != "" && name != "." && name != ".." && !strings.ContainsAny(name, `/\`) && len(name) <= 253
}

// Server serves a substrate's objects to other identities.
type Server struct {
	Key      ed25519.PrivateKey
	Zone     string // the zone of the substrate, the start of every path
	Store    Store
	Resolver *Resolver
	// Links returns the declared links; nil means none are declared. An
	// error refuses the transfer: an unknown network is not an open one.
	Links func(ctx context.Context) ([]plan.Link, error)
	// Logf, when set, records every decision.
	Logf func(format string, args ...any)
}

func (s *Server) logf(format string, args ...any) {
	if s.Logf != nil {
		s.Logf(format, args...)
	}
}

// Path of an object on the direct channel.
const objectPath = "/physis/v1/objects/"

// authorize finds an object and decides whether peer may have it.
func (s *Server) authorize(ctx context.Context, peer ID, name string) (*Object, int, error) {
	o, err := s.Store.Get(name)
	if err != nil {
		return nil, http.StatusNotFound, err
	}
	to, err := s.Resolver.Resolve(ctx, peer)
	if err != nil {
		// Only registered substrates receive data: an identity with no
		// substrate has no jurisdiction to check.
		return nil, http.StatusForbidden, fmt.Errorf("%w: %v", ErrRefused, err)
	}
	var links []plan.Link
	if s.Links != nil {
		if links, err = s.Links(ctx); err != nil {
			return nil, http.StatusForbidden, fmt.Errorf("%w: links unknown: %v", ErrRefused, err)
		}
	}
	if err := AllowSend(o.Jurisdictions, links, Record{ID: Of(s.Key), Zone: s.Zone}, to); err != nil {
		return nil, http.StatusForbidden, err
	}
	return o, http.StatusOK, nil
}

// Handler serves GET /physis/v1/objects/{name} to authenticated peers. It
// supports byte ranges, so a transfer that breaks resumes where it stopped;
// the ETag is the content digest, so a resumed range is from the same bytes.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET "+objectPath+"{name}", func(w http.ResponseWriter, r *http.Request) {
		if r.TLS == nil || len(r.TLS.PeerCertificates) == 0 {
			http.Error(w, "an identity is required", http.StatusUnauthorized)
			return
		}
		peer, err := peerID([][]byte{r.TLS.PeerCertificates[0].Raw})
		if err != nil {
			http.Error(w, err.Error(), http.StatusUnauthorized)
			return
		}
		name := r.PathValue("name")
		o, code, err := s.authorize(r.Context(), peer, name)
		if err != nil {
			s.logf("identity: %s asked for %s: %v", peer.Short(), name, err)
			http.Error(w, err.Error(), code)
			return
		}
		d := o.Digest()
		w.Header().Set("ETag", `"`+d+`"`)
		w.Header().Set("Content-Type", "application/octet-stream")
		http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(o.Data))
	})
	return mux
}

// Listener is a running direct endpoint.
type Listener struct {
	srv *http.Server
	ln  net.Listener
}

// Addr is the address the listener is bound to.
func (l *Listener) Addr() string { return l.ln.Addr().String() }

// Close stops the listener and drops its connections.
func (l *Listener) Close() error { return l.srv.Close() }

// Start serves the direct channel on addr (host:port; port 0 picks one).
func (s *Server) Start(addr string) (*Listener, error) {
	cfg, err := serverTLS(s.Key)
	if err != nil {
		return nil, err
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}
	srv := &http.Server{Handler: s.Handler(), ReadHeaderTimeout: 10 * time.Second,
		ErrorLog: log.New(discard{}, "", 0)}
	go srv.Serve(tls.NewListener(ln, cfg))
	return &Listener{srv: srv, ln: ln}, nil
}

// discard silences the TLS handshake errors of peers that are refused,
// which are expected (a caller hitting a stale endpoint, a scanner).
type discard struct{}

func (discard) Write(p []byte) (int, error) { return len(p), nil }
