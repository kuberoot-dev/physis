package identity

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"fmt"
	"math/big"
	"time"
)

// The direct channel is TLS 1.3 with self-signed certificates whose public
// key is the identity key itself, on both sides. Nothing about the
// certificate is trusted except that key: no CA, no name, no validity
// period. TLS 1.3 already is a challenge-response: each side signs the
// handshake transcript, which contains the other side's fresh random, with
// the private key of its certificate (CertificateVerify), and Go checks that
// signature against the certificate's key whether or not the chain is
// verified. So a peer that presents identity X and completes the handshake
// holds X's private key; the channel is encrypted and bound to it.
//
// This was chosen over signing payloads on plain HTTP because it gets
// confidentiality, replay protection and integrity of every byte from the
// standard library, with one check of ours: that the key in the
// certificate is the identity we wanted.

// certificate makes a self-signed certificate for the identity key.
func certificate(priv ed25519.PrivateKey) (tls.Certificate, error) {
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 127))
	if err != nil {
		return tls.Certificate{}, err
	}
	now := time.Now()
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: Of(priv).String()},
		NotBefore:    now.Add(-time.Hour),
		NotAfter:     now.Add(365 * 24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, priv.Public(), priv)
	if err != nil {
		return tls.Certificate{}, err
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: priv}, nil
}

// peerID reads the identity of a peer's leaf certificate.
func peerID(rawCerts [][]byte) (ID, error) {
	if len(rawCerts) == 0 {
		return ID{}, errors.New("identity: peer presented no certificate")
	}
	c, err := x509.ParseCertificate(rawCerts[0])
	if err != nil {
		return ID{}, fmt.Errorf("identity: peer certificate: %w", err)
	}
	pub, ok := c.PublicKey.(ed25519.PublicKey)
	if !ok {
		return ID{}, errors.New("identity: peer certificate key is not Ed25519")
	}
	return FromPublicKey(pub)
}

// ErrWrongPeer is returned when the peer at an endpoint is not the identity
// the caller asked for: the endpoint is stale or forged.
var ErrWrongPeer = errors.New("identity: endpoint is held by another identity")

// serverTLS accepts any client that proves an Ed25519 identity; what the
// client may do is decided per request from its identity.
func serverTLS(priv ed25519.PrivateKey) (*tls.Config, error) {
	cert, err := certificate(priv)
	if err != nil {
		return nil, err
	}
	return &tls.Config{
		MinVersion:   tls.VersionTLS13,
		Certificates: []tls.Certificate{cert},
		ClientAuth:   tls.RequireAnyClientCert,
		NextProtos:   []string{"http/1.1"},
		VerifyPeerCertificate: func(raw [][]byte, _ [][]*x509.Certificate) error {
			_, err := peerID(raw)
			return err
		},
	}, nil
}

// clientTLS connects only to the given identity, presenting the caller's.
// InsecureSkipVerify turns off the CA and host-name checks that do not
// apply here; VerifyConnection, which runs regardless, pins the key. There
// is no session cache, so every connection is a full handshake in which the
// peer signs anew.
func clientTLS(priv ed25519.PrivateKey, want ID) (*tls.Config, error) {
	cert, err := certificate(priv)
	if err != nil {
		return nil, err
	}
	return &tls.Config{
		MinVersion:         tls.VersionTLS13,
		Certificates:       []tls.Certificate{cert},
		InsecureSkipVerify: true,
		NextProtos:         []string{"http/1.1"},
		VerifyConnection: func(cs tls.ConnectionState) error {
			if len(cs.PeerCertificates) == 0 {
				return ErrWrongPeer
			}
			pub, ok := cs.PeerCertificates[0].PublicKey.(ed25519.PublicKey)
			if !ok {
				return ErrWrongPeer
			}
			got, err := FromPublicKey(pub)
			if err != nil || got != want {
				return fmt.Errorf("%w: wanted %s, found %s", ErrWrongPeer, want.Short(), got.Short())
			}
			return nil
		},
	}, nil
}
