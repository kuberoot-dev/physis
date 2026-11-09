package main

import (
	"context"
	"crypto/ed25519"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"strings"

	v1 "github.com/kuberoot-dev/physis/api/v1alpha1"
	"github.com/kuberoot-dev/physis/pkg/identity"
)

// Addressing by identity (docs/network.md). Off by default: without -listen
// the agent publishes no endpoints and opens no port.
var (
	listen     = flag.String("listen", "", "address of the identity endpoint, e.g. :7443 (empty: off). It serves the files of -objects over TLS keyed by the agent's signing key, to other substrates' identities, and is published in status.endpoints")
	advertise  = flag.String("advertise", os.Getenv("PHYSIS_ADVERTISE"), "host:port published as the endpoint (default: the address of -listen, or $POD_IP or the host name when it listens on all addresses)")
	objectsDir = flag.String("objects", "", "directory whose files the identity endpoint serves (default: <state>/objects)")
	objectJur  = flag.String("object-jurisdictions", "", "comma-separated jurisdictions the served files may be sent to (empty: any registered substrate)")
	fetchAddr  = flag.String("fetch", "", "fetch physis:<identity>/<object> with the agent's key, write it to -fetch-out and exit")
	fetchOut   = flag.String("fetch-out", "", "file for -fetch (default: standard output)")
)

// directory reads identity records from the Substrate objects.
func (a *agent) directory() identity.Directory {
	return identity.DirectoryFunc(func(ctx context.Context) ([]identity.Record, error) {
		subs, err := a.c.ListSubstrates(ctx)
		if err != nil {
			return nil, err
		}
		var out []identity.Record
		for _, s := range subs {
			if r, ok := identity.RecordOf(s.Meta.Name, s.Spec, s.Status); ok {
				out = append(out, r)
			}
		}
		return out, nil
	})
}

// serveIdentity starts the identity endpoint and returns what the
// heartbeat publishes.
func (a *agent) serveIdentity(priv ed25519.PrivateKey) []v1.Endpoint {
	dir := *objectsDir
	if dir == "" {
		dir = filepath.Join(*stateDir, "objects")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		log.Fatal(err)
	}
	srv := &identity.Server{Key: priv, Zone: *zone, Resolver: &identity.Resolver{Dir: a.directory()},
		Store: identity.DirStore{Dir: dir, Jurisdictions: splitList(*objectJur)},
		Links: a.c.ListLinks, Logf: log.Printf}
	ln, err := srv.Start(*listen)
	if err != nil {
		log.Fatalf("identity endpoint: %v", err)
	}
	addr := *advertise
	if addr == "" {
		// A listener bound to one address is reachable there; one bound
		// to all addresses is published under the pod IP or host name.
		host, port, _ := net.SplitHostPort(ln.Addr())
		if ip := net.ParseIP(host); ip == nil || ip.IsUnspecified() {
			if host = os.Getenv("POD_IP"); host == "" {
				host = hostname()
			}
		}
		addr = net.JoinHostPort(host, port)
	}
	log.Printf("identity %s serves %s on %s, published as %s", identity.Of(priv), dir, ln.Addr(), addr)
	return []v1.Endpoint{{Transport: v1.TransportHTTPS, Address: addr}}
}

// fetchOnce fetches physis:<identity>/<object> and writes it out.
func (a *agent) fetchOnce(ctx context.Context, priv ed25519.PrivateKey) error {
	i := strings.LastIndexByte(*fetchAddr, '/')
	if i < 0 {
		return fmt.Errorf("-fetch wants physis:<identity>/<object>, got %q", *fetchAddr)
	}
	from, err := identity.Parse((*fetchAddr)[:i])
	if err != nil {
		return err
	}
	c := &identity.Client{Key: priv, Resolver: &identity.Resolver{Dir: a.directory()}}
	f, err := c.Fetch(ctx, from, (*fetchAddr)[i+1:])
	if err != nil {
		return err
	}
	for _, e := range f.Failures {
		log.Printf("fetch: retried after: %v", e)
	}
	log.Printf("fetched %d bytes, %s, via %s", len(f.Data), f.Digest, f.Via)
	if *fetchOut != "" {
		return os.WriteFile(*fetchOut, f.Data, 0o600)
	}
	_, err = os.Stdout.Write(f.Data)
	return err
}

func splitList(s string) []string {
	var out []string
	for _, x := range strings.Split(s, ",") {
		if x = strings.TrimSpace(x); x != "" {
			out = append(out, x)
		}
	}
	return out
}
