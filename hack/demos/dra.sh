#!/usr/bin/env bash
# Screencast: a DRA ResourceSlice becomes a physis substrate on the kuberoot cluster.
set -euo pipefail; cd "$(dirname "$0")/../.."; . hack/demos/lib.sh
say "A DRA driver publishes a device; it states its power, not its speed"
show "k get resourceslice rack-cpu -o jsonpath='{.spec.driver} {.spec.devices[0].name} {.spec.devices[0].attributes}' | cut -c1-200; echo"
say "physis-dra-bridge turns it into a substrate: what the driver stated is cited to it, the rest is marked as an assumption"
show "k get substrate dra-rack-7-cpu0 -o jsonpath='{range .spec.model[*]}{.name}={.value}  {.source}{\"\\n\"}{end}'"
say "An agent pod adopts the substrate without changing its description; a problem allowed only under CH lands on it"
show "k -n dra-cluster get problem via-dra -o custom-columns=PHASE:.status.phase,SUBSTRATE:.status.claimedBy,QUALITY:.status.result.quality,RECEIPT:.status.result.receipt"
