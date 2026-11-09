//go:build !unix

package run

// cpuSeconds has no portable source outside Unix (Windows, WASI); callers
// fall back to wall time when it reports zero.
func cpuSeconds() float64 { return 0 }
