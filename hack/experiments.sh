#!/usr/bin/env bash
# Runs every simulation experiment of the article. Results go to results/sim.
set -euo pipefail
cd "$(dirname "$0")/.."
go build -o out/physis-sim ./cmd/physis-sim
for q in 2026q1 2026q3; do
  out/physis-sim -grid data/grid/$q -cal out/calibration -seeds 5 -rate 4 -out results/sim/$q/main
  out/physis-sim -grid data/grid/$q -cal out/calibration -seeds 3 -rate 4 -out results/sim/$q/sensitivity -experiment sensitivity
  out/physis-sim -grid data/grid/$q -cal out/calibration -seeds 3 -rate 4 -out results/sim/$q/mismatch -experiment mismatch
  out/physis-sim -grid data/grid/$q -cal out/calibration -seeds 3 -rate 4 -out results/sim/$q/data -experiment data
done
echo EXPERIMENTS-DONE
