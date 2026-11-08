#!/usr/bin/env bash
# Runs the simulation experiments in parallel, one process per experiment.
cd "$(dirname "$0")/.."
S=out/physis-sim
for spec in "2026q1 main 5" "2026q1 sensitivity 3" "2026q1 mismatch 3" "2026q1 data 3" "2026q3 main 5" "2026q3 sensitivity 3" "2026q3 mismatch 3" "2026q3 data 3"; do
  set -- $spec
  ( $S -grid data/grid/$1 -cal out/calibration -seeds $3 -rate 4 -out results/sim/$1/$2 -experiment $2 > out/exp-$1-$2.log 2>&1; echo DONE >> out/exp-$1-$2.log ) &
done
wait
