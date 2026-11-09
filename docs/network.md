# Network: addressing by identity

The design (`docs/design.ru.md`, section 4.4) and the article (section
"Сеть") describe the network of the new layer in seven points. Until now the
prototype implemented the priced `Link` and the jurisdiction of paths;
addressing by identity and mutual authentication were design only. This
document describes the part of them that is now implemented, in
`pkg/identity`, how it is wired into the agent, what it proves and what it
does not, and what remains design.

## What is implemented

### An identity is the substrate's key

Every agent already has an Ed25519 key: it signs the receipts, it is
published in `Substrate.status.publicKey`, and it survives restarts and moves
(state directory or Secret). That key is now also the substrate's network
identity. Nothing new has to be issued, and the identity that moves data is
the same one that signs results.

The address format is

```text
physis:<base32 of the 32-byte key and a 2-byte checksum>
```

for example `physis:mvmz7oylrc5wkjhfmz2jlai2kn3tuxjp37nxegq2tpv42btcy2ek7zq`
(55 characters after the scheme, lowercase). The address carries the whole
key, not a prefix of it, so it is self-certifying: checking that a peer is
the identity named by an address needs no lookup and no trust in DNS, in an
IP address or in the API server. The checksum turns a mistyped address into
an error instead of the name of some other key. `identity.ID` marshals to
JSON as its address.

### Resolution: identity to endpoints

An agent started with `-listen` publishes where it can be reached now:

```yaml
status:
  publicKey: 65592fbb...
  endpoints:
  - {transport: https, address: 10.0.3.7:7443}
```

Two transports are defined: `https`, the agent's own listener, and
`api-relay`, meaning reachable only through small messages exchanged via the
API server. `status` is `x-kubernetes-preserve-unknown-fields`, so the CRDs
did not change.

`identity.Resolver` maps an identity to the record of the substrate that
holds it: substrate name, zone, jurisdiction, endpoints. It reads the
Substrate objects on every call and caches nothing, because a stale answer
is exactly what a caller re-resolving after a move must not get. Two rules:

- a retired key (`status.retiredKeys`) does not resolve; it verifies old
  receipts and reaches nothing;
- a key claimed by two substrates is an error, not a choice: the
  jurisdiction of the identity would be ambiguous, and copying someone's
  public key into another Substrate must not redirect anything.

Endpoints are hints, not trust. Every connection authenticates the key, so a
stale or forged endpoint fails closed.

### Mutual authentication

Two mechanisms, one per channel, both standard-library Ed25519:

- **Direct channel: TLS 1.3 with the identity key as the certificate key.**
  Each side presents a self-signed certificate whose public key is its
  identity key. Nothing in the certificate is trusted except that key: no
  CA, no name, no validity period. TLS 1.3 is itself a challenge-response:
  each side signs the handshake transcript, which includes the other side's
  fresh random, with its certificate's private key (CertificateVerify), and
  Go verifies that signature against the certificate's key even when chain
  verification is off. The caller pins the expected identity in
  `VerifyConnection`; the server requires a client certificate and learns
  the caller's identity from it.
- **Explicit challenge-response** (`identity.Respond`, `VerifyResponse`):
  the callee signs the caller's 32-byte nonce bound to both identities and
  to a purpose string, so an answer cannot be replayed to another caller,
  attributed to another callee or confused with a receipt signature. The
  relay uses the same construction for its messages.

Why TLS rather than signed payloads over plain HTTP: TLS gives
confidentiality, replay protection and integrity of every byte, from the
standard library, with exactly one check of ours (the key in the
certificate is the identity we asked for). Signing payloads on plain HTTP
would need its own replay protection, would leave data readable on the
path, and would sign each response separately.

### Moving a named object by identity

`identity.Server` serves `GET /physis/v1/objects/{name}` on the direct
channel; `identity.Client.Fetch(ctx, id, name)` fetches it:

1. resolve the identity to its endpoints;
2. connect to the `https` endpoint and check the peer's key;
3. ask for the object in ranges (1 MiB by default); the first answer
   announces the content digest (`ETag: "sha256:..."`), later ranges are
   asked with `If-Range` on that digest, so a resumed transfer is from the
   same bytes or fails as "changed during the transfer";
4. on any failure (connection refused, connection dropped, or the endpoint
   now held by another key), resolve the identity again and continue from
   the last byte received; a refusal by the authenticated peer (policy,
   missing object) is final;
5. check the whole object against the digest.

So when B moves (new IP, restart elsewhere, or its old address handed to
another machine), A follows the identity, not the address, and loses at most
one range. The test `TestFetchFollowsIdentityAcrossMove` does exactly this
with two in-process agents and a third identity that takes B's old port and
serves different bytes under the same name; the log of the run shows the
retry reason, "endpoint is held by another identity".

`Fetched` reports how the transfer went: transport used, resolutions,
retries and their causes.

### The relay fallback

An identity whose only endpoint is `api-relay` (an agent behind NAT that
reaches the API server but accepts no connections) is fetched through a
mailbox of small signed messages: A posts a request signed by A with a fresh
nonce; B takes requests addressed to it, checks A's signature, applies the
same policy as the direct channel, and posts an answer signed by B that
echoes the nonce and carries the data and its digest. A accepts only an
answer from the identity it asked, for its nonce, with a valid signature
and matching digest. Answers are limited to 256 KiB (`MaxRelayBytes`), well
under what the API server stores in one object.

The protocol is implemented and tested against `identity.Relay`, an
interface, with an in-memory implementation (`MemRelay`). The
implementation backed by API objects is **not** written and the agent does
not publish `api-relay`; see "What remains design".

### Policy on paths

The node that sends decides, by the receiver's authenticated identity, never
by its address. `identity.AllowSend` is the planner's rule for data paths
(`pkg/plan`) applied to a receiver that has just proven who it is:

- the receiver's operator jurisdiction must be among the jurisdictions the
  object allows (normally those of the problem it belongs to);
- when `Link` objects are declared, a link must connect the sender's and
  the receiver's zones, and its path must cross only allowed jurisdictions;
  same zone needs no link;
- an identity that no substrate holds receives nothing;
- if the links cannot be read, the transfer is refused: an unknown network
  is not an open one.

`plan.FindLink` and `plan.Link.Forbidden` are now exported and used by both
the planner and this policy, so the two cannot drift apart.

### In the agent

Off by default: without `-listen` the agent opens no port and publishes no
endpoints, so existing stands and the conformance suite are unchanged.

| Flag | Meaning |
|---|---|
| `-listen :7443` | start the identity endpoint (TLS keyed by the agent's signing key) |
| `-advertise host:port` | the address published in `status.endpoints` (default: the listen address, or `$POD_IP` / host name when listening on all addresses) |
| `-objects dir` | the files served as named objects (default `<state>/objects`) |
| `-object-jurisdictions EU,CH` | where those files may be sent (empty: any registered substrate) |
| `-fetch physis:<id>/<name>` `-fetch-out file` | fetch one object with the agent's key and exit |

The agent's ClusterRole gains `get, list, watch` on `links` (in
`deploy/rbac.yaml` and the chart), for the path policy.

A stand run (envtest control plane with no nodes, three agents, B in DE/EU
serving a 3 MB file to EU only): A in FR/EU fetched it and the bytes
matched; C in GB/UK was refused with "substrate c is under jurisdiction UK,
the data allows [EU]"; B was restarted on another port, published the new
endpoint, and A fetched again by the same address.

## What this proves and what it does not

- A peer that completes the handshake holds the identity's private key. An
  API server, a DNS server or a router that lies about endpoints can deny
  service but cannot make A accept bytes from anyone but B, or make B send
  to anyone but the identity that authenticated.
- The jurisdiction of an identity is what its Substrate declares. The
  authentication proves who the receiver is, not where it is; the claim is
  as good as the operator who registered the substrate. Attestation of
  location is outside the prototype.
- Relay messages are signed, not encrypted: whoever can read the mailbox
  sees the data.
- Objects are held in memory whole; the store is a directory of files. This
  is a prototype of the protocol, not a data plane.
- There is no revocation besides key rotation, which retires the old key.

## Tests

`go test ./pkg/identity`:

| Test | What it checks |
|---|---|
| `TestAddressRoundTrip`, `TestAddressRejectsMalformed` | address format, JSON form, checksum catches a typo |
| `TestChallengeResponse` | honest answer verifies; a forged key, another nonce, another caller, a truncated or purpose-less signature fail |
| `TestResolve` | resolution from substrate records; unknown and retired keys; a key claimed twice |
| `TestAllowSend` | receiver jurisdiction, missing link, path through a forbidden jurisdiction, same zone |
| `TestFetchFollowsIdentityAcrossMove` | two in-process agents; B moves mid-transfer, a third identity takes its old address; A re-resolves and finishes with B's bytes |
| `TestFetchRefusesWrongPeer` | an endpoint held by another key never yields data |
| `TestServerAuthenticatesCaller` | unregistered caller, receiver outside the data's jurisdictions, path through UK, unreadable links, missing object |
| `TestFetchSmallAndEmptyObjects`, `TestFetchDetectsChangedObject` | edge cases of ranged transfer |
| `TestRelayFallback`, `TestRelayRejectsForgery` | relay transfer, size limit, policy through the relay, forged answers and requests ignored |

## What remains design

- **API-backed relay.** One small object per message, deleted on take, with
  RBAC limited to the agent's namespace; then the agent can publish
  `api-relay`. The protocol and its limit are ready for it. Relay payloads
  should be encrypted to the receiver's key.
- **Transfers as plan steps.** The planner prices moving data and waits for
  it, but does not yet order a fetch; nothing ties a problem's dataset to
  the object store, and objects are named, not yet addressed by digest end to
  end.
- **Workload identities.** Only substrates have identities. Problems,
  datasets and people's mandates would need their own, and an interoperable
  form (SPIFFE IDs and SVIDs) for existing service meshes.
- **Transport by context.** One transport exists (HTTP/1.1 over TLS 1.3
  over TCP). QUIC between sites, RPC transports like Homa inside a data
  centre, multipath, and the choice among them by the application's delivery
  semantics are design.
- **Delay-tolerant delivery.** Ranged resume survives a break of seconds to
  minutes. Store-and-forward custody over links that are absent for hours
  (Bundle Protocol v7) is design.
- **Replacing kube-proxy and the CNI.** The identity endpoint runs over
  whatever IP network the site has. Pod networking, Services and
  NetworkPolicy are untouched; enforcement of identity policy in the
  datapath (for example with eBPF), and the absence of a cluster-wide flat
  address space, are design.
- **Proof of location.** Jurisdiction is declared, not attested.

## Relation to the article's network section

| Point of the article | Status |
|---|---|
| 1. Addressing by identity, not by IP | Implemented for substrates: address, resolution, re-resolution after a move. Workloads and data: design |
| 2. Link as an object with a price | Implemented earlier (`Link`, TC-17) |
| 3. Data movement as a planned operation | Priced and timed by the planner; the transfer itself now exists between agents; triggering it from a plan and content addressing end to end: design |
| 4. Transport by context | Design |
| 5. Policy by purpose and jurisdiction | Implemented in the planner and now also at the sending agent, by authenticated identity |
| 6. Mutual authentication by default | Implemented on the identity channel (TLS 1.3 with identity keys, signed relay messages); the rest of the cluster network is unchanged |
| 7. No global flat address space | Partly: the identity channel needs only that the endpoint be reachable, or the API server for the relay; the cluster's own pod network stays as it is |

The migration path the article describes, "first links and the price of
moving as objects next to the existing network, then data movement as a
plan step, then identity replacing IP in policies and receipts", moves one
step: identity now replaces IP for moving data between substrates and for
the decision whether to send it.
