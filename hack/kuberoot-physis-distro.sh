#!/usr/bin/env bash
# Builds the container-free physis distribution of kuberoot (arm64) in local
# containers, after hack/kuberoot-local-build.sh has built the kernel and the
# base binaries into the krwork volume. physis runs as node services: no
# kubelet, no containerd, no pods. Output: krwork:/work/out/arm64/media-physis.img
set -euo pipefail
cd "$(dirname "$0")/.."
SRC=$PWD/.stand/kuberoot
ARCH=arm64
STAGE=out/kuberoot-physis/root
mkdir -p $STAGE/usr/bin $STAGE/usr/share/physis/data
# The distribution itself, from this repository into the kuberoot tree.
mkdir -p "$SRC/distros/physis"
cp -r deploy/kuberoot-distro/. "$SRC/distros/physis/"
# physis binaries, calibration and data.
for c in physis-scheduler physis-agent; do
  GOOS=linux GOARCH=$ARCH CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o $STAGE/usr/bin/$c ./cmd/$c
done
cp -r data/gset data/digits $STAGE/usr/share/physis/data/
cp results/best-found.json $STAGE/usr/share/physis/data/
cp results/learn.json $STAGE/usr/share/physis/learn.json
docker run --rm -v krwork:/work alpine:3.22 sh -c "mkdir -p /work/out/$ARCH/physis"
tar -C out/kuberoot-physis -cf - root | docker run --rm -i -v krwork:/work alpine:3.22 tar -C /work/out/$ARCH/physis -xf -
ENV="-e KUBEROOT_OUT=/work/out -e WORK=/work -e JOBS=6 -e GOMAXPROCS=6 -e KUBEROOT_DISTRO=physis"
docker run --rm -v krwork:/work -v "$SRC":/src -w /src $ENV alpine:3.22 sh scripts/build-rootfs.sh $ARCH
docker run --rm -v krwork:/work -v "$SRC":/src -w /src $ENV golang:1.26-bookworm bash -c \
  "command -v cpio >/dev/null || { apt-get -qq update && apt-get -qq install -y cpio zstd >/dev/null; }; scripts/build-initramfs.sh $ARCH"
docker run --rm -v krwork:/work -v "$SRC":/src -w /src $ENV golang:1.26-bookworm bash -c \
  "go run ./cmd/mkimage -arch $ARCH -dev -artifacts /work/out/$ARCH -trust /work/trust -out /work/out/$ARCH/media-physis.img && ls -lh /work/out/$ARCH/media-physis.img"
