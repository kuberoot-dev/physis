#!/usr/bin/env bash
# Boots the locally built kuberoot edge media in QEMU with HVF on macOS: a
# single node that becomes a cluster of one. Node API on localhost:50000,
# cluster API on localhost:6443, serial console in out/kuberoot/console.log.
#   hack/kuberoot-local-boot.sh fetch   copy media and trust out of the build volume
#   hack/kuberoot-local-boot.sh up|down
set -euo pipefail
cd "$(dirname "$0")/.."
D=out/kuberoot
FW=/opt/homebrew/share/qemu/edk2-aarch64-code.fd
case "${1:-}" in
  fetch)
    mkdir -p $D
    docker run --rm -v krwork:/work alpine:3.22 cat /work/out/arm64/media.img > $D/media.img
    docker run --rm -v krwork:/work alpine:3.22 tar -C /work -cf - trust | tar -C $D -xf -
    ls -lh $D/media.img; ls $D/trust ;;
  up)
    [ -f $D/system.qcow2 ] || qemu-img create -f qcow2 $D/system.qcow2 20G >/dev/null
    [ -f $D/vars.fd ] || dd if=/dev/zero of=$D/vars.fd bs=1m count=64 2>/dev/null
    nohup qemu-system-aarch64 -machine virt -accel hvf -cpu host -smp 4 -m 5120 \
      -drive if=pflash,format=raw,readonly=on,file=$FW -drive if=pflash,format=raw,file=$D/vars.fd \
      -drive file=$D/system.qcow2,if=virtio -drive file=$D/media.img,if=virtio,format=raw \
      -netdev user,id=n0,hostfwd=tcp::50000-:50000,hostfwd=tcp::6443-:6443 -device virtio-net-pci,netdev=n0 -device virtio-rng-pci \
      -display none -serial file:$D/console.log > $D/qemu.log 2>&1 &
    echo $! > $D/qemu.pid; echo "booting, console: $D/console.log" ;;
  down)
    [ -f $D/qemu.pid ] && kill "$(cat $D/qemu.pid)" 2>/dev/null || true ;;
  *) echo "usage: $0 fetch|up|down"; exit 1 ;;
esac
