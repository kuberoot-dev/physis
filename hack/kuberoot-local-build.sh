#!/usr/bin/env bash
# Builds kuberoot (edge, arm64) in local containers -- the same steps as the
# project's builder pod -- so physis can be tested on kuberoot without a
# shared cluster. Sources: .stand/kuberoot. Output: docker volume krwork.
set -euo pipefail
cd "$(dirname "$0")/.."
SRC=$PWD/.stand/kuberoot
ENV="-e KUBEROOT_OUT=/work/out -e WORK=/work -e JOBS=6 -e GOMAXPROCS=6 -e KUBEROOT_DISTRO=edge"
tools() { docker run --rm -v krwork:/work -v "$SRC":/src -w /src $ENV golang:1.26-bookworm bash -c "$*"; }
alpine() { docker run --rm -v krwork:/work -v "$SRC":/src -w /src $ENV alpine:3.22 sh -c "$*"; }
PREP='command -v flex >/dev/null || { apt-get -qq update && apt-get -qq install -y flex bison bc libelf-dev libssl-dev cpio xz-utils zstd kmod dwarves bsdextrautils >/dev/null; }'
step() { echo "=== $1 $(date +%T)"; }
step kernel;   tools "$PREP; [ -f /work/out/arm64/vmlinuz.efi ] || scripts/build-kernel.sh arm64"
step drbd;     tools "$PREP; scripts/build-drbd.sh arm64"
step binaries; tools "$PREP; scripts/build-binaries.sh arm64"
step rootfs;   alpine "sh scripts/build-rootfs.sh arm64"
step initramfs; tools "$PREP; scripts/build-initramfs.sh arm64"
step media;    tools "go run ./cmd/mkimage -arch arm64 -dev -artifacts /work/out/arm64 -trust /work/trust -out /work/out/arm64/media.img && ls -lh /work/out/arm64/media.img"
step done
