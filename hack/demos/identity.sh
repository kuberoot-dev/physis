#!/usr/bin/env bash
# Screencast: substrates address each other by key, across nodes of the kuberoot cluster.
set -euo pipefail; cd "$(dirname "$0")/../.."; . hack/demos/lib.sh
say "Substrate B on one node serves a 3 MB dataset allowed only in the EU; its address is its key"
show "k get substrate id-b-de -o custom-columns=NAME:.metadata.name,ZONE:.spec.zone,JURISDICTION:.spec.jurisdiction,ENDPOINT:.status.endpoints[0].address"
show "go run ./cmd/physis-identity-addr \$(k get substrate id-b-de -o jsonpath='{.status.publicKey}')"
say "A (FR, EU) on another node fetches it by that address: TLS keyed by the identity, hash checked"
show "k -n physis-system logs id-a-fr-fetch -c agent | grep fetched | cut -c1-140"
say "C (GB, UK) asks too and is refused by policy, decided on its authenticated identity"
show "k -n physis-system logs id-c-gb-fetch -c agent | grep refused | cut -c21-200"
