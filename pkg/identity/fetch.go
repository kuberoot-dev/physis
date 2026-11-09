package identity

import (
	"context"
	"crypto/ed25519"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	v1 "github.com/kuberoot-dev/physis/api/v1alpha1"
)

// Client fetches objects from identities.
type Client struct {
	Key      ed25519.PrivateKey
	Resolver *Resolver
	// Relay is used when the identity has no direct endpoint; nil turns
	// the fallback off.
	Relay Relay
	// ChunkSize is the size of one ranged request (default 1 MiB). A
	// broken transfer loses at most one chunk.
	ChunkSize int64
	// Attempts per chunk before giving up (default 5); every attempt after
	// the first resolves the identity again.
	Attempts int
	// Backoff between attempts (default 200 ms).
	Backoff time.Duration
	// RelayWait bounds the wait for an answer through the relay (default
	// 30 s).
	RelayWait time.Duration
	// OnChunk, when set, is called after every chunk received directly.
	OnChunk func(done, total int64)
}

// Fetched is the result of a fetch and how it went.
type Fetched struct {
	Data   []byte
	Digest string
	// Via is the transport of the last chunk: https or api-relay.
	Via string
	// Resolutions counts lookups of the identity; Retries counts failed
	// attempts that were retried, and Failures says why they failed.
	Resolutions, Retries int
	Failures             []error
}

// ErrNoEndpoint is returned when the identity has no endpoint the client
// can use.
var ErrNoEndpoint = errors.New("identity: no usable endpoint")

// permanent marks errors that another attempt will not change: the peer
// was reached, authenticated and said no.
type permanent struct{ error }

func (p permanent) Unwrap() error { return p.error }

func (c *Client) defaults() {
	if c.ChunkSize <= 0 {
		c.ChunkSize = 1 << 20
	}
	if c.Attempts <= 0 {
		c.Attempts = 5
	}
	if c.Backoff == 0 {
		c.Backoff = 200 * time.Millisecond
	}
	if c.RelayWait <= 0 {
		c.RelayWait = 30 * time.Second
	}
}

// Fetch gets a named object from an identity. It resolves the identity to
// its current endpoints, connects to one, and checks that the peer holds
// the identity's key before asking for anything. When the endpoint stops
// answering or turns out to be held by someone else (the substrate moved,
// restarted elsewhere, or the address was given to another machine), the
// client resolves the identity again and continues from the last byte it
// has. The result is checked against the content digest the peer
// announced at the first chunk.
func (c *Client) Fetch(ctx context.Context, from ID, name string) (*Fetched, error) {
	c.defaults()
	tc, err := clientTLS(c.Key, from)
	if err != nil {
		return nil, err
	}
	tr := &http.Transport{TLSClientConfig: tc, ForceAttemptHTTP2: false, ResponseHeaderTimeout: time.Minute}
	defer tr.CloseIdleConnections()
	hc := &http.Client{Transport: tr}

	f := &Fetched{}
	var rec Record
	total := int64(-1)
	for total < 0 || int64(len(f.Data)) < total {
		var last error
		ok := false
		for attempt := 0; attempt < c.Attempts && !ok; attempt++ {
			if attempt > 0 {
				f.Retries++
				f.Failures = append(f.Failures, last)
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-time.After(c.Backoff):
				}
				// A stale connection to the old endpoint must not be reused.
				tr.CloseIdleConnections()
			}
			if attempt > 0 || f.Resolutions == 0 {
				f.Resolutions++
				if rec, err = c.Resolver.Resolve(ctx, from); err != nil {
					last = err
					continue
				}
			}
			ep, direct := rec.Endpoint(v1.TransportHTTPS)
			if !direct {
				if _, relay := rec.Endpoint(v1.TransportRelay); relay && c.Relay != nil {
					data, err := c.viaRelay(ctx, from, name)
					if err != nil {
						return nil, err
					}
					if f.Digest != "" && Digest(data) != f.Digest {
						return nil, fmt.Errorf("identity: %s changed during the transfer", name)
					}
					f.Data, f.Digest, f.Via = data, Digest(data), v1.TransportRelay
					return f, nil
				}
				last = fmt.Errorf("%w for %s (substrate %s)", ErrNoEndpoint, from.Short(), rec.Substrate)
				continue
			}
			chunk, size, etag, err := c.chunk(ctx, hc, ep.Address, name, int64(len(f.Data)), f.Digest)
			if err != nil {
				var p permanent
				if errors.As(err, &p) {
					return nil, p.error
				}
				last = err
				continue
			}
			if len(chunk) == 0 && size > int64(len(f.Data)) {
				return nil, fmt.Errorf("identity: %s: empty range before the end", name)
			}
			if f.Digest == "" {
				f.Digest = etag
			}
			f.Data = append(f.Data, chunk...)
			f.Via = v1.TransportHTTPS
			total = size
			ok = true
			if c.OnChunk != nil {
				c.OnChunk(int64(len(f.Data)), total)
			}
		}
		if !ok {
			return nil, fmt.Errorf("identity: fetch %s from %s: %w", name, from.Short(), last)
		}
	}
	if Digest(f.Data) != f.Digest {
		return nil, fmt.Errorf("identity: %s does not match its digest %s", name, f.Digest)
	}
	return f, nil
}

// chunk asks for the bytes of name from offset on, at most one chunk. It
// returns the bytes, the size of the whole object and its digest.
func (c *Client) chunk(ctx context.Context, hc *http.Client, addr, name string, offset int64, digest string) ([]byte, int64, string, error) {
	u := "https://" + addr + objectPath + url.PathEscape(name)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, 0, "", permanent{err}
	}
	req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", offset, offset+c.ChunkSize-1))
	if digest != "" {
		// If the object changed since the first chunk, the server answers
		// with the whole new object instead of a range: detected below.
		req.Header.Set("If-Range", `"`+digest+`"`)
	}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, 0, "", err
	}
	defer resp.Body.Close()
	etag := strings.Trim(resp.Header.Get("ETag"), `"`)
	switch resp.StatusCode {
	case http.StatusPartialContent:
		size, err := rangeTotal(resp.Header.Get("Content-Range"))
		if err != nil {
			return nil, 0, "", err
		}
		body, err := io.ReadAll(io.LimitReader(resp.Body, c.ChunkSize))
		if err != nil {
			return nil, 0, "", err
		}
		return body, size, etag, nil
	case http.StatusOK:
		if offset > 0 || (digest != "" && etag != digest) {
			return nil, 0, "", permanent{fmt.Errorf("identity: %s changed during the transfer", name)}
		}
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, 0, "", err
		}
		return body, int64(len(body)), etag, nil
	case http.StatusRequestedRangeNotSatisfiable:
		// An empty object has no byte 0.
		if size, err := rangeTotal(resp.Header.Get("Content-Range")); err == nil && size == 0 && offset == 0 {
			return nil, 0, etag, nil
		}
		return nil, 0, "", permanent{fmt.Errorf("identity: %s: range not satisfiable", name)}
	default:
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		text := strings.TrimSpace(string(msg))
		var err error
		switch resp.StatusCode {
		case http.StatusNotFound:
			err = fmt.Errorf("%w: %s", ErrNoObject, name)
		case http.StatusForbidden:
			if err = remoteError(text); !errors.Is(err, ErrRefused) {
				err = fmt.Errorf("%w: %s", ErrRefused, text)
			}
		default:
			err = fmt.Errorf("identity: %s: %s: %s", name, resp.Status, text)
		}
		if resp.StatusCode >= 500 {
			return nil, 0, "", err
		}
		return nil, 0, "", permanent{err}
	}
}

// rangeTotal reads the size from "bytes a-b/size" or "bytes */size".
func rangeTotal(h string) (int64, error) {
	i := strings.LastIndexByte(h, '/')
	if i < 0 {
		return 0, fmt.Errorf("identity: bad Content-Range %q", h)
	}
	return strconv.ParseInt(h[i+1:], 10, 64)
}
