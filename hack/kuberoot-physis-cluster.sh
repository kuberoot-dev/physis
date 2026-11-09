#!/usr/bin/env bash
# Builds the container-free physis distribution of kuberoot (amd64) in the
# builder pod of a workshop-cluster tenant and boots one node from it, so
# nothing heavy runs on the laptop. Uses kuberoot's hack/hetzner.sh from the
# kuberoot tree in .stand/kuberoot.
#   KUBEROOT_NS=tenant-physis hack/kuberoot-physis-cluster.sh build|node <name>
set -euo pipefail
cd "$(dirname "$0")/.."
NS=${KUBEROOT_NS:-tenant-physis}; ARCH=amd64; KR=.stand/kuberoot
export KUBECONFIG=${KUBECONFIG_WORKSHOP:-$HOME/workshop.txt}
k() { kubectl --context admin@workshop -n "$NS" "$@"; }
case "${1:-}" in
  build)
    mkdir -p "$KR/distros/physis" out/kuberoot-physis-$ARCH/root/usr/bin out/kuberoot-physis-$ARCH/root/usr/share/physis/data
    cp -r deploy/kuberoot-distro/. "$KR/distros/physis/"
    R=out/kuberoot-physis-$ARCH/root
    for c in physis-scheduler physis-agent; do
      GOOS=linux GOARCH=$ARCH CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o $R/usr/bin/$c ./cmd/$c
    done
    cp -r data/gset data/digits results/best-found.json $R/usr/share/physis/data/
    cp results/learn.json $R/usr/share/physis/learn.json
    k exec -i builder -c tools -- mkdir -p /work/out/$ARCH/physis
    tar -C out/kuberoot-physis-$ARCH -cf - root | k exec -i builder -c tools -- tar -xf - -C /work/out/$ARCH/physis
    if [ "${REBUILD_BINARIES:-1}" = 1 ]; then
      (cd "$KR" && KUBEROOT_NS=$NS KUBEROOT_DISTRO=physis JOBS=8 GOMAXPROCS=8 hack/hetzner.sh build $ARCH)
    else
      # Only the profile or physis changed: rootfs and initramfs from the
      # binaries already built.
      (cd "$KR" && KUBEROOT_NS=$NS hack/hetzner.sh sync)
      k exec -i builder -c alpine -- sh -c "KUBEROOT_OUT=/work/out WORK=/work KUBEROOT_DISTRO=physis sh /work/src/kuberoot/scripts/build-rootfs.sh $ARCH"
      k exec -i builder -c tools -- sh -c "KUBEROOT_OUT=/work/out WORK=/work /work/src/kuberoot/scripts/build-initramfs.sh $ARCH"
    fi
    (cd "$KR" && KUBEROOT_NS=$NS hack/hetzner.sh media $ARCH) ;;
  node)
    (cd "$KR" && KUBEROOT_NS=$NS hack/hetzner.sh node "${2:?name}" $ARCH) ;;
  *) echo "usage: $0 build|node <name>"; exit 1 ;;
esac
