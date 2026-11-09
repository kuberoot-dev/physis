// Command physis-calibrate runs every algorithm on every physics it applies
// to, over the experiment's instances and several seeds, and writes:
//   - the learned quality distributions (learn.Store JSON) the scheduler uses;
//   - runs.csv: one row per run with operation counts, host CPU time and the
//     device model's time and energy, the raw material of the article's
//     tables.
//
// The annealing dynamics are the same algorithm on the CPU, the digital
// annealer, the SFQ and the adiabatic machine, so one execution per seed is
// recorded under each of those physics.
package main

import (
	"encoding/csv"
	"flag"
	"fmt"
	"log"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"time"

	"github.com/kuberoot-dev/physis/pkg/device"
	"github.com/kuberoot-dev/physis/pkg/learn"
	"github.com/kuberoot-dev/physis/pkg/plan"
	"github.com/kuberoot-dev/physis/pkg/run"
	"github.com/kuberoot-dev/physis/pkg/workload"
)

func main() {
	out := flag.String("out", "out/calibration", "output directory")
	only := flag.String("physics", "", "calibrate only this physics (empty: all that can run here)")
	seeds := flag.Int("seeds", 5, "seeds per instance")
	quick := flag.Bool("quick", false, "smaller instance set for development")
	flag.Parse()
	if err := os.MkdirAll(*out, 0o755); err != nil {
		log.Fatal(err)
	}
	gnames := []string{"G1", "G2", "G22", "G11", "G14", "G43"}
	if *quick {
		gnames = []string{"G1", "G11"}
	}
	gs, err := workload.Gset("data/gset", gnames...)
	if err != nil {
		log.Fatal(err)
	}
	small := workload.SmallRandom([]int{12, 16, 20}, 0.4, 4)
	if *quick {
		small = workload.SmallRandom([]int{12, 16}, 0.4, 2)
	}
	digits, err := workload.Digits("data/digits", []int{100, 500, 1797}, 0.3)
	if err != nil {
		log.Fatal(err)
	}
	inst := append(append(append([]workload.Named{}, small...), gs...), digits...)

	host, _ := os.Hostname()
	cpuModel := cpuName()
	f, err := os.Create(filepath.Join(*out, "runs.csv"))
	if err != nil {
		log.Fatal(err)
	}
	w := csv.NewWriter(f)
	w.Write([]string{"instance", "ref_source", "class", "n", "algo", "effort", "physics", "emulated", "seed", "quality", "value",
		"spin_updates", "macs", "host_cpu_s", "host_wall_s", "device_s", "device_j", "host", "cpu_model"})
	store := learn.NewStore()
	type rec struct {
		in      workload.Named
		algo, e string
		phys    device.Physics
		seed    int
		r       *run.Result
	}
	var recs []rec
	start := time.Now()
	for _, in := range inst {
		shape := in.Shape()
		for _, a := range plan.Catalog {
			if a.Class != shape.Class {
				continue
			}
			for _, e := range a.Efforts {
				// One execution per seed, recorded under every physics that
				// runs the same dynamics.
				var phys []device.Physics
				for _, p := range a.Physics {
					// Hosted physics run only where their hardware is.
					if p == device.GPU && run.GPUWorker == "" {
						continue
					}
					if *only != "" && string(p) != *only {
						continue
					}
					if plan.CanRun(device.Builtin(p), a, shape) {
						phys = append(phys, p)
					}
				}
				if len(phys) == 0 {
					continue
				}
				ns := *seeds
				if a.Name == "exact" || a.Name == "exact-fp" || a.Name == "exact-gpu" {
					ns = 1
				}
				for s := 1; s <= ns; s++ {
					// Physics with their own noise model (analog vs photonic)
					// need their own execution.
					shared := a.Name != "analog"
					var r *run.Result
					for _, p := range phys {
						m := device.Builtin(p)
						if r == nil || !shared {
							r, err = run.Execute(m, a.Name, e.Name, in.Instance, uint64(s))
							if err != nil {
								log.Fatalf("%s %s/%s on %s: %v", in.Name, a.Name, e.Name, p, err)
							}
						}
						rr := *r
						if m.Emulated {
							rr.Device = m.Predict(r.Ops)
						} else if p == device.GPU {
							// Measured by the worker on the GPU itself.
						} else {
							secs := math.Max(r.HostCPUSeconds, 1e-6)
							rr.Device = device.Cost{Seconds: secs, Joules: secs * m.ActivePowerW.V}
						}
						recs = append(recs, rec{in, a.Name, e.Name, p, s, &rr})
					}
				}
			}
		}
		log.Printf("%-24s done (%s)", in.Name, time.Since(start).Round(time.Second))
	}
	// References for instances without one: the best cut found anywhere.
	best := map[string]float64{}
	for _, r := range recs {
		if r.in.Graph != nil && r.r.Value > best[r.in.Name] {
			best[r.in.Name] = r.r.Value
		}
	}
	for _, r := range recs {
		q := r.r.Quality
		src := r.in.RefSource
		if r.in.Graph != nil && r.in.Reference == 0 {
			q = r.r.Value / best[r.in.Name]
			src = "best found"
		}
		shape := r.in.Shape()
		store.Observe(learn.Key{Class: string(shape.Class), Algo: r.algo, Effort: r.e, Physics: string(r.phys), SizeBucket: learn.Bucket(shape.Size())}, q)
		m := device.Builtin(r.phys)
		w.Write([]string{r.in.Name, src, string(shape.Class), strconv.Itoa(shape.N), r.algo, r.e, string(r.phys), strconv.FormatBool(m.Emulated),
			strconv.Itoa(r.seed), fmt.Sprintf("%.6f", q), fmt.Sprintf("%g", r.r.Value), strconv.FormatInt(r.r.Ops.SpinUpdates, 10),
			strconv.FormatInt(r.r.Ops.MACs, 10), fmt.Sprintf("%.6f", r.r.HostCPUSeconds), fmt.Sprintf("%.6f", r.r.HostWall.Seconds()),
			fmt.Sprintf("%.6g", r.r.Device.Seconds), fmt.Sprintf("%.6g", r.r.Device.Joules), host, cpuModel})
	}
	w.Flush()
	f.Close()
	sf, err := os.Create(filepath.Join(*out, "learn.json"))
	if err != nil {
		log.Fatal(err)
	}
	store.Save(sf)
	sf.Close()
	log.Printf("%d runs, %d quality keys, %s on %s (%s, GOMAXPROCS=%d)", len(recs), len(store.Keys()), time.Since(start).Round(time.Second), host, cpuModel, runtime.GOMAXPROCS(0))
}

func cpuName() string {
	if b, err := os.ReadFile("/proc/cpuinfo"); err == nil {
		for _, line := range splitLines(string(b)) {
			if len(line) > 10 && line[:10] == "model name" {
				for i := range line {
					if line[i] == ':' {
						return trim(line[i+1:])
					}
				}
			}
		}
	}
	return runtime.GOOS + "/" + runtime.GOARCH
}

func splitLines(s string) []string {
	var out []string
	st := 0
	for i := range s {
		if s[i] == '\n' {
			out = append(out, s[st:i])
			st = i + 1
		}
	}
	return out
}

func trim(s string) string {
	for len(s) > 0 && (s[0] == ' ' || s[0] == '\t') {
		s = s[1:]
	}
	return s
}
