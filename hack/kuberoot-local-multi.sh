#!/usr/bin/env bash
# Boots a two-node kuberoot edge cluster in QEMU with HVF on macOS. Each node
# has two links: eth0 on QEMU user networking (DHCP, the way out, the node API
# forwarded to the host) and eth1 on a point-to-point socket link between the
# two machines, with the static cluster address baked into its media
# (mkimage -args kuberoot.ip=eth1:10.0.0.N/24). Node a (10.0.0.11) becomes the
# control plane; node b (10.0.0.12) joins it as a worker with a JoinTicket.
#   hack/kuberoot-local-multi.sh fetch   copy both media and trust from the build volume
#   hack/kuberoot-local-multi.sh up|down|join
set -euo pipefail
cd "$(dirname "$0")/.."
D=out/kuberoot-2n
FW=/opt/homebrew/share/qemu/edk2-aarch64-code.fd
boot() { # name ip nodeapi-port extra-hostfwd socket-netdev
  local n=$1 ip=$2 port=$3 fwd=$4 sock=$5
  [ -f $D/$n/system.qcow2 ] || qemu-img create -f qcow2 $D/$n/system.qcow2 20G >/dev/null
  [ -f $D/$n/vars.fd ] || dd if=/dev/zero of=$D/$n/vars.fd bs=1m count=64 2>/dev/null
  nohup qemu-system-aarch64 -machine virt -accel hvf -cpu host -smp 3 -m 3584 \
    -drive if=pflash,format=raw,readonly=on,file=$FW -drive if=pflash,format=raw,file=$D/$n/vars.fd \
    -drive file=$D/$n/system.qcow2,if=virtio -drive file=$D/$n/media.img,if=virtio,format=raw \
    -netdev user,id=n0,hostfwd=tcp::$port-:50000$fwd -device virtio-net-pci,netdev=n0,mac=52:54:00:00:00:$ip \
    -netdev socket,id=n1,$sock -device virtio-net-pci,netdev=n1,mac=52:54:00:00:01:$ip -device virtio-rng-pci \
    -display none -serial file:$D/$n/console.log > $D/$n/qemu.log 2>&1 &
  echo $! > $D/$n/qemu.pid
}
node() { kubectl --kubeconfig $D/trust/admin.kubeconfig --server "https://127.0.0.1:$1" "${@:2}"; }
case "${1:-}" in
  fetch)
    mkdir -p $D/a $D/b
    docker run --rm -v krwork:/work alpine:3.22 cat /work/out/arm64/media-11.img > $D/a/media.img
    docker run --rm -v krwork:/work alpine:3.22 cat /work/out/arm64/media-12.img > $D/b/media.img
    docker run --rm -v krwork:/work alpine:3.22 tar -C /work -cf - trust | tar -C $D -xf -
    ls -lh $D/a/media.img $D/b/media.img ;;
  up)
    boot a 11 50010 ",hostfwd=tcp::6444-:6443" "listen=127.0.0.1:7711"
    sleep 2
    boot b 12 50011 "" "connect=127.0.0.1:7711"
    echo "booting; consoles: $D/a/console.log $D/b/console.log" ;;
  join)
    # The node's name comes from the last three bytes of its eth0 MAC.
    name=kuberoot-000012
    printf 'apiVersion: node.kuberoot.dev/v1alpha1\nkind: JoinTicket\nmetadata: {name: %s}\nspec: {nodeName: %s}\n' "$name" "$name" \
      | node 50010 create -f - -o jsonpath='{.status.membership}' > $D/membership.yaml
    node 50011 apply -f $D/membership.yaml ;;
  down)
    for n in a b; do [ -f $D/$n/qemu.pid ] && kill "$(cat $D/$n/qemu.pid)" 2>/dev/null || true; done ;;
  *) echo "usage: $0 fetch|up|join|down"; exit 1 ;;
esac
