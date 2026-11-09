# Sourced by build-rootfs.sh: physis binaries, calibration and data, staged
# into $OUT/physis/root by physis/hack/kuberoot-physis-distro.sh.
if [ ! -d "$OUT/physis/root" ]; then echo "physis: nothing staged in $OUT/physis/root" >&2; exit 1; fi
cp -a "$OUT/physis/root/." $r/
