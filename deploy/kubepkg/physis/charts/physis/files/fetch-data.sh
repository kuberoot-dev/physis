#!/bin/sh
# Init container script: fetches the benchmark inputs into /data.
set -eu
mkdir -p /data/gset /data/digits
for g in G1 G2 G11 G14 G22 G43; do
  [ -s /data/gset/$g ] || wget -q -O /data/gset/$g "https://web.stanford.edu/~yyye/yyye/Gset/$g"
done
for f in optdigits.tra optdigits.tes; do
  [ -s /data/digits/$f ] || wget -q -O /data/digits/$f "https://archive.ics.uci.edu/ml/machine-learning-databases/optdigits/$f"
done
