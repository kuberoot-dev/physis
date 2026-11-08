// Command physis-sim evaluates scheduling policies on the digital twin: a
// quarter of real grid data, a fleet of substrates of eleven physics in four
// countries, and a stream of problems. It writes per-policy summaries over
// several seeds (mean and 5-95% range) as CSV and JSON.
package main

import (
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math"
	"math/rand/v2"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/kuberoot-dev/physis/pkg/device"
	"github.com/kuberoot-dev/physis/pkg/grid"
	"github.com/kuberoot-dev/physis/pkg/learn"
	"github.com/kuberoot-dev/physis/pkg/plan"
	"github.com/kuberoot-dev/physis/pkg/sim"
)

var zones = []string{"DE", "FR", "PL", "GB"}

// dataPerSample is the size of one classification input; the data-volume
// experiment raises it to see when moving data stops paying off.
var dataPerSample = 65.0

// fleet places one machine of each special physics in a country, the way a
// market of owners would, and a CPU pool in every country.
func fleet() []*sim.Member {
	var f []*sim.Member
	for _, z := range zones {
		m := device.Builtin(device.CPU)
		f = append(f, &sim.Member{Name: "cpu-" + strings.ToLower(z), Zone: z, Model: m, True: m, Slots: 16})
	}
	place := map[device.Physics]string{
		device.IsingDigital: "PL", device.SFQ: "DE", device.Adiabatic: "FR", device.PBit: "GB",
		device.OpticalIsing: "GB", device.QPU: "DE", device.AnalogIMC: "FR", device.Photonic: "DE",
		device.Neuromorphic: "PL", device.Organoid: "FR",
	}
	var ps []device.Physics
	for p := range place {
		ps = append(ps, p)
	}
	sort.Slice(ps, func(i, j int) bool { return ps[i] < ps[j] })
	for _, p := range ps {
		m := device.Builtin(p)
		f = append(f, &sim.Member{Name: string(p) + "-" + strings.ToLower(place[p]), Zone: place[p], Model: m, True: m, Slots: 1})
	}
	return f
}

type inst struct {
	name  string
	shape plan.Shape
}

func instances() []inst {
	return []inst{
		{"G1", plan.Shape{Class: plan.MaxCut, N: 800, DegreeSum: 2 * 19176}},
		{"G2", plan.Shape{Class: plan.MaxCut, N: 800, DegreeSum: 2 * 19176}},
		{"G11", plan.Shape{Class: plan.MaxCut, N: 800, DegreeSum: 2 * 1600}},
		{"G14", plan.Shape{Class: plan.MaxCut, N: 800, DegreeSum: 2 * 4694}},
		{"G43", plan.Shape{Class: plan.MaxCut, N: 1000, DegreeSum: 2 * 9990}},
		{"G22", plan.Shape{Class: plan.MaxCut, N: 2000, DegreeSum: 2 * 19990}},
		{"er16", plan.Shape{Class: plan.MaxCut, N: 16, DegreeSum: 96}},
		{"er20", plan.Shape{Class: plan.MaxCut, N: 20, DegreeSum: 152}},
		{"digits100", plan.Shape{Class: plan.Classify, N: 100, DegreeSum: 640}},
		{"digits500", plan.Shape{Class: plan.Classify, N: 500, DegreeSum: 640}},
		{"digits1797", plan.Shape{Class: plan.Classify, N: 1797, DegreeSum: 640}},
	}
}

// jobs draws a stream: arrivals follow a daily cycle (twice as many during
// European working hours), half interactive (60 s deadline, may not wait),
// half batch (2-24 h deadline, may wait). A fifth of jobs may only run in the
// EU, a tenth only in their data's zone.
func jobs(from, to time.Time, perHour float64, seed uint64) []sim.Job {
	rng := rand.New(rand.NewPCG(seed, 7))
	ins := instances()
	var js []sim.Job
	id := 0
	for t := from; t.Before(to.Add(-26 * time.Hour)); {
		h := t.Hour()
		rate := perHour
		if h >= 7 && h < 19 {
			rate *= 2
		}
		t = t.Add(time.Duration(rng.ExpFloat64() / rate * float64(time.Hour)))
		in := ins[rng.IntN(len(ins))]
		r := plan.Request{ID: strconv.Itoa(id), Shape: in.shape, Confidence: 0.9}
		if in.shape.Class == plan.MaxCut {
			r.Quality = []float64{0.98, 0.99, 0.995}[rng.IntN(3)]
			if in.shape.N <= 20 {
				r.Quality = []float64{0.99, 1.0}[rng.IntN(2)]
			}
			r.DataBytes = float64(in.shape.DegreeSum) * 12
		} else {
			r.Quality = []float64{0.97, 0.99, 0.999}[rng.IntN(3)]
			// The UCI digits are 8x8 images of 64 bytes plus a label.
			r.DataBytes = float64(in.shape.N) * dataPerSample
		}
		r.DataZone = zones[rng.IntN(len(zones))]
		if rng.Float64() < 0.5 {
			r.Deadline = t.Add(60 * time.Second)
		} else {
			r.Deadline = t.Add(time.Duration(2+rng.Float64()*22) * time.Hour)
			r.Deferrable = true
		}
		switch x := rng.Float64(); {
		case x < 0.2:
			r.Zones = []string{"DE", "FR", "PL"}
		case x < 0.3:
			r.Zones = []string{r.DataZone}
		}
		js = append(js, sim.Job{ID: id, Arrival: t, Instance: in.name, Shape: in.shape, Req: r})
		id++
	}
	return js
}

func cpuRates(runsCSV string) (map[string]float64, error) {
	f, err := os.Open(runsCSV)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	rows, err := csv.NewReader(f).ReadAll()
	if err != nil {
		return nil, err
	}
	col := map[string]int{}
	for i, h := range rows[0] {
		col[h] = i
	}
	ops := map[string]float64{}
	secs := map[string]float64{}
	for _, r := range rows[1:] {
		if r[col["physics"]] != "cpu" {
			continue
		}
		s, _ := strconv.ParseFloat(r[col["host_cpu_s"]], 64)
		u, _ := strconv.ParseFloat(r[col["spin_updates"]], 64)
		m, _ := strconv.ParseFloat(r[col["macs"]], 64)
		if s < 1e-4 {
			continue // too short to time reliably
		}
		k := "spin"
		if r[col["class"]] == "classify" {
			k = "mac"
		}
		ops[k] += u + m
		secs[k] += s
	}
	out := map[string]float64{}
	for k := range ops {
		out[k] = ops[k] / secs[k]
	}
	return out, nil
}

func policies() []sim.Policy {
	all := []device.Physics(nil)
	cpu := []device.Physics{device.CPU}
	return []sim.Policy{
		{Name: "k8s-today", Objective: plan.Fastest, Physics: cpu, LocalOnly: true},
		{Name: "carbon-aware-cpu", Objective: plan.MinCarbon, Physics: cpu, Defer: true},
		{Name: "hetero-fastest", Objective: plan.Fastest, Physics: all},
		{Name: "physis-energy", Objective: plan.MinEnergy, Physics: all, Learn: true},
		{Name: "physis-carbon", Objective: plan.MinCarbon, Physics: all, Defer: true, Learn: true},
		{Name: "physis-cost", Objective: plan.MinCost, Physics: all, Defer: true, Learn: true},
		{Name: "oracle-carbon", Objective: plan.MinCarbon, Physics: all, Defer: true, UseActuals: true, Learn: true},
	}
}

func main() {
	gridDir := flag.String("grid", "data/grid/2026q3", "grid data directory")
	calDir := flag.String("cal", "out/calibration", "calibration directory")
	out := flag.String("out", "out/sim", "output directory")
	seeds := flag.Int("seeds", 5, "seeds")
	perHour := flag.Float64("rate", 20, "base arrivals per hour")
	flag.Float64Var(&dataPerSample, "sample-bytes", 65, "bytes per classification sample")
	flag.Parse()
	g, err := grid.LoadDir(*gridDir, zones)
	if err != nil {
		log.Fatal(err)
	}
	lf, err := os.Open(filepath.Join(*calDir, "learn.json"))
	if err != nil {
		log.Fatal(err)
	}
	store, err := learn.Load(lf)
	lf.Close()
	if err != nil {
		log.Fatal(err)
	}
	rates, err := cpuRates(filepath.Join(*calDir, "runs.csv"))
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("CPU rates (ops/s): %v", rates)
	cal := &sim.Calibration{Store: store, CPUOpsPerSecond: rates}
	s0 := g.Zones["DE"]
	from, to := s0.Start, s0.End()
	if err := os.MkdirAll(*out, 0o755); err != nil {
		log.Fatal(err)
	}
	var all []sim.Summary
	for _, p := range policies() {
		for s := 1; s <= *seeds; s++ {
			js := jobs(from, to, *perHour, uint64(s))
			t0 := time.Now()
			os := sim.Run(sim.Config{Fleet: fleet(), Grid: g, Cal: cal, Policy: p, Seed: uint64(s), Noise: 0.1,
				InterZoneJPerByte: interZone, IntraZoneJPerByte: interZone / 10, PUE: 1.2}, js)
			sm := sim.Summarise(p.Name, os)
			if s == 1 {
				why := map[string]int{}
				for _, o := range os {
					if !o.Placed {
						why[fmt.Sprintf("%s q=%.3f: %s", o.Instance, o.Quality0, o.Reason)]++
					}
				}
				type kv struct {
					k string
					n int
				}
				var top []kv
				for k, n := range why {
					top = append(top, kv{k, n})
				}
				sort.Slice(top, func(i, j int) bool { return top[i].n > top[j].n })
				for i := 0; i < len(top) && i < 4; i++ {
					log.Printf("   unplaced x%d: %s", top[i].n, top[i].k)
				}
			}
			all = append(all, sm)
			log.Printf("%-17s seed %d: %d jobs, %.2f kWh, %.3f kgCO2, EUR %.3f (+%d unpriced), DL miss %.2f%%, Q miss %.2f%%, wait %.0f min (%s)",
				p.Name, s, sm.Jobs, sm.KWh, sm.KgCO2, sm.EUR, sm.EURUnknown, 100*sm.DeadlineMiss, 100*sm.QualityMiss, sm.MeanWaitMin, time.Since(t0).Round(time.Second))
		}
	}
	writeSummary(*out, all)
}

// Energy of moving data between zones: 0.06 kWh/GB in 2015, halving every
// two years (ref:aslan2018), extrapolated to 2026: 0.06 / 2^5.5 kWh/GB.
var interZone = 0.06 / math.Pow(2, 5.5) * 3.6e6 / 1e9

func writeSummary(dir string, all []sim.Summary) {
	b, _ := json.MarshalIndent(all, "", " ")
	os.WriteFile(filepath.Join(dir, "summaries.json"), b, 0o644)
	by := map[string][]sim.Summary{}
	var order []string
	for _, s := range all {
		if _, ok := by[s.Policy]; !ok {
			order = append(order, s.Policy)
		}
		by[s.Policy] = append(by[s.Policy], s)
	}
	f, _ := os.Create(filepath.Join(dir, "table.csv"))
	defer f.Close()
	w := csv.NewWriter(f)
	w.Write([]string{"policy", "kwh_mean", "kwh_p5", "kwh_p95", "kgco2_mean", "kgco2_p5", "kgco2_p95", "eur_mean", "deadline_miss_pct", "quality_miss_pct", "wait_min", "placed_pct", "physics_share"})
	for _, p := range order {
		ss := by[p]
		col := func(f func(sim.Summary) float64) []float64 {
			var v []float64
			for _, s := range ss {
				v = append(v, f(s))
			}
			return v
		}
		kwh := col(func(s sim.Summary) float64 { return s.KWh })
		co2 := col(func(s sim.Summary) float64 { return s.KgCO2 })
		qk := sim.Quantiles(kwh, 0.05, 0.95)
		qc := sim.Quantiles(co2, 0.05, 0.95)
		share := map[device.Physics]float64{}
		for _, s := range ss {
			for k, v := range s.PhysicsShare {
				share[k] += v / float64(len(ss))
			}
		}
		var parts []string
		for k, v := range share {
			parts = append(parts, fmt.Sprintf("%s:%.1f%%", k, 100*v))
		}
		sort.Strings(parts)
		w.Write([]string{p, f3(mean(kwh)), f3(qk[0]), f3(qk[1]), f3(mean(co2)), f3(qc[0]), f3(qc[1]),
			f3(mean(col(func(s sim.Summary) float64 { return s.EUR }))),
			f3(100 * mean(col(func(s sim.Summary) float64 { return s.DeadlineMiss }))),
			f3(100 * mean(col(func(s sim.Summary) float64 { return s.QualityMiss }))),
			f3(mean(col(func(s sim.Summary) float64 { return s.MeanWaitMin }))),
			f3(100 * mean(col(func(s sim.Summary) float64 { return float64(s.Placed) / float64(s.Jobs) }))),
			strings.Join(parts, " ")})
	}
	w.Flush()
}

func f3(x float64) string { return strconv.FormatFloat(x, 'f', 4, 64) }

func mean(x []float64) float64 {
	var t float64
	for _, v := range x {
		t += v
	}
	return t / float64(len(x))
}
