// Package identity addresses substrates by who they are, not by where they
// are. An identity is the Ed25519 public key a substrate's agent already
// signs its receipts with (Substrate.status.publicKey). The package gives:
//
//   - an address format for that key, physis:<base32>, that is
//     self-certifying: the address contains the whole key, so checking a peer
//     needs no lookup and no trust in DNS, IP or the API server;
//   - a resolver from an identity to the endpoints where it can be reached
//     now, read from Substrate.status.endpoints;
//   - mutual authentication by challenge-response: a peer proves it holds the
//     key behind the identity by signing a fresh nonce of the caller's;
//   - moving a named object from one identity to another, directly over TLS
//     1.3 whose certificates carry the identity keys, or through small signed
//     messages relayed via the API when there is no direct endpoint;
//   - a policy that refuses to send to an identity whose substrate's
//     jurisdiction, or the path to it, the data does not allow.
//
// Endpoints are hints. Every connection authenticates the key, so an
// endpoint that went stale or was forged fails closed, and the caller
// re-resolves and tries again.
package identity

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

// Scheme prefixes every address.
const Scheme = "physis:"

// ID is an identity: an Ed25519 public key.
type ID [ed25519.PublicKeySize]byte

var b32 = base32.StdEncoding.WithPadding(base32.NoPadding)

// checksumLen bytes of a hash follow the key in an address, so a mistyped
// address is reported as malformed instead of naming another key.
const checksumLen = 2

func checksum(id ID) []byte {
	h := sha256.Sum256(append([]byte("physis-address-v1"), id[:]...))
	return h[:checksumLen]
}

// FromPublicKey makes an identity of a public key.
func FromPublicKey(pub ed25519.PublicKey) (ID, error) {
	var id ID
	if len(pub) != ed25519.PublicKeySize {
		return id, fmt.Errorf("identity: key of %d bytes, want %d", len(pub), ed25519.PublicKeySize)
	}
	copy(id[:], pub)
	return id, nil
}

// Of returns the identity of a private key.
func Of(priv ed25519.PrivateKey) ID {
	id, _ := FromPublicKey(priv.Public().(ed25519.PublicKey))
	return id
}

// FromHex reads a key in the hex form of Substrate.status.publicKey.
func FromHex(s string) (ID, error) {
	b, err := hex.DecodeString(s)
	if err != nil {
		return ID{}, fmt.Errorf("identity: %w", err)
	}
	return FromPublicKey(b)
}

// Hex is the form used in Substrate.status.publicKey and receipts.
func (id ID) Hex() string { return hex.EncodeToString(id[:]) }

// PublicKey returns the key.
func (id ID) PublicKey() ed25519.PublicKey { return ed25519.PublicKey(id[:]) }

// String is the address: physis: followed by the lowercase base32 of the
// key and a two-byte checksum.
func (id ID) String() string {
	return Scheme + strings.ToLower(b32.EncodeToString(append(id[:], checksum(id)...)))
}

// Short is a prefix of the address for logs; it identifies nothing.
func (id ID) Short() string { return id.String()[:len(Scheme)+12] }

// Parse reads an address.
func Parse(addr string) (ID, error) {
	var id ID
	rest, ok := strings.CutPrefix(addr, Scheme)
	if !ok {
		return id, fmt.Errorf("identity: %q does not start with %s", addr, Scheme)
	}
	b, err := b32.DecodeString(strings.ToUpper(rest))
	if err != nil || len(b) != len(id)+checksumLen {
		return id, fmt.Errorf("identity: malformed address %q", addr)
	}
	copy(id[:], b)
	if string(b[len(id):]) != string(checksum(id)) {
		return id, fmt.Errorf("identity: bad checksum in %q", addr)
	}
	return id, nil
}

// MarshalText writes the address, so identities in JSON read as addresses.
func (id ID) MarshalText() ([]byte, error) { return []byte(id.String()), nil }

// UnmarshalText reads an address.
func (id *ID) UnmarshalText(b []byte) error {
	v, err := Parse(string(b))
	if err != nil {
		return err
	}
	*id = v
	return nil
}

// message is what is signed for a purpose: a domain-separation prefix and
// every part with its length, so no two different inputs sign the same
// bytes and a signature made for one purpose is useless for another.
func message(purpose string, parts ...[]byte) []byte {
	m := []byte("physis/identity/v1/" + purpose + "\x00")
	for _, p := range parts {
		m = binary.BigEndian.AppendUint32(m, uint32(len(p)))
		m = append(m, p...)
	}
	return m
}

func sign(priv ed25519.PrivateKey, purpose string, parts ...[]byte) []byte {
	return ed25519.Sign(priv, message(purpose, parts...))
}

func verify(id ID, purpose string, sig []byte, parts ...[]byte) bool {
	return len(sig) == ed25519.SignatureSize && ed25519.Verify(id.PublicKey(), message(purpose, parts...), sig)
}

// Nonce is a caller's fresh challenge.
type Nonce [32]byte

// NewNonce draws a random nonce.
func NewNonce() (Nonce, error) {
	var n Nonce
	_, err := rand.Read(n[:])
	return n, err
}

// MarshalText writes the nonce in hex.
func (n Nonce) MarshalText() ([]byte, error) { return []byte(hex.EncodeToString(n[:])), nil }

// UnmarshalText reads a nonce in hex.
func (n *Nonce) UnmarshalText(b []byte) error {
	v, err := hex.DecodeString(string(b))
	if err != nil || len(v) != len(n) {
		return errors.New("identity: malformed nonce")
	}
	copy(n[:], v)
	return nil
}

// ErrNotProven is returned when a peer fails to prove its identity.
var ErrNotProven = errors.New("identity: peer did not prove the key behind the identity")

// Respond answers a caller's challenge: the holder of priv signs the nonce
// bound to both identities, so the answer cannot be replayed to another
// caller or passed off as another identity's.
func Respond(priv ed25519.PrivateKey, caller ID, n Nonce) []byte {
	callee := Of(priv)
	return sign(priv, "challenge", caller[:], callee[:], n[:])
}

// VerifyResponse checks that callee answered caller's nonce.
func VerifyResponse(callee, caller ID, n Nonce, sig []byte) error {
	if !verify(callee, "challenge", sig, caller[:], callee[:], n[:]) {
		return ErrNotProven
	}
	return nil
}
