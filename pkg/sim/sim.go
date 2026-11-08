// Package sim is the digital twin (principle P6): it replays a stream of
// problems against a fleet of substrates over real grid data, with the same
// scheduler code that runs on the cluster. The scheduler decides on
// forecasts; each run's outcome is drawn from what calibration actually
// observed (quality samples, measured or modelled cost, with run-to-run
// noise) and charged at the actual carbon intensity and price of the hours
// it ran in.
package sim

import (
	"math"
	"math/rand/v2"
	"sort"
	"time"

	"github.com/kuberoot-dev/physis/pkg/device"
	"github.com/kuberoot-dev/physis/pkg/grid"
	"github.com/kuberoot-dev/physis/pkg/learn"
	"github.com/kuberoot-dev/physis/pkg/plan"
	"github.com/kuberoot-dev/physis/pkg/solve"
)

// Member is a substrate of the fleet with a number of parallel slots.
type Member struct {
	Name  string
	Zone  string
	Model *device.Model
	// True is the model the world actually follows; the scheduler starts
	// from Model. They differ in the model-mismatch experiment.
	True  *device.Model
	Slots int
	busy  []time.Time
}

// Job is one problem in the stream.
type Job struct {
	ID       int
	Arrival  time.Time
	Instance string
	Shape    plan.Shape
	Req      plan.Request
}

// Outcome of one job under one policy.
type Outcome struct {
	Job       int
	Placed    bool
	Physics   device.Physics
	Zone      string
	Algorithm string
	Start     time.Time
	Wait      time.Duration
	Seconds   float64
	Joules    float64
	GCO2      float64
	EUR       float64 // NaN where the zone has no published price
	Quality   float64
	MissDL    bool
	MissQ     bool
	PredJ     float64
	Reason    string // why it was not placed
	Instance  string
	Quality0  float64 // the target
}

// Policy turns a job's request into what the scheduler sees.
type Policy struct {
	Name       string
	Objective  plan.Policy
	Physics    []device.Physics // empty = all
	Defer      bool
	LocalOnly  bool // run only in the data's zone (one-cluster world)
	UseActuals bool // oracle
	Learn      bool // update corrections from outcomes
}

// Calibration supplies observed quality and per-op host cost.
type Calibration struct {
	Store *learn.Store
	// CPUOpsPerSecond is the measured spin-update (or MAC) rate of the host
	// CPU per algorithm, from runs.csv.
	CPUOpsPerSecond map[string]float64
}

// Config of a run.
type Config struct {
	Fleet  []*Member
	Grid   *grid.Provider
	Cal    *Calibration
	Policy Policy
	Seed   uint64
	// Noise is the log-normal sigma of run-to-run cost variation.
	Noise             float64
	InterZoneJPerByte float64
	IntraZoneJPerByte float64
	PUE               float64
}

// Run replays jobs in arrival order.
func Run(cfg Config, jobs []Job) []Outcome {
	rng := rand.New(rand.NewPCG(cfg.Seed, cfg.Seed^0x5bd1e995))
	store := cfg.Cal.Store
	learnStore := learn.NewStore()
	// The scheduler's own corrections start empty; quality comes from
	// calibration.
	learnStore.Quality = store.Quality
	for _, m := range cfg.Fleet {
		m.busy = make([]time.Time, max(m.Slots, 1))
	}
	out := make([]Outcome, 0, len(jobs))
	for _, j := range jobs {
		r := j.Req
		if cfg.Policy.LocalOnly && r.DataZone != "" {
			r.Zones = []string{r.DataZone}
		}
		r.Policy = cfg.Policy.Objective
		r.Deferrable = r.Deferrable && cfg.Policy.Defer
		if len(cfg.Policy.Physics) > 0 {
			r.Pin = &plan.PinAlgorithm{Physics: cfg.Policy.Physics}
		}
		var subs []*plan.Substrate
		idx := map[string]*Member{}
		for _, m := range cfg.Fleet {
			free := m.busy[0]
			for _, b := range m.busy[1:] {
				if b.Before(free) {
					free = b
				}
			}
			mm := *m.Model
			if m.Model.Physics == device.CPU {
				if rate, ok := cfg.Cal.CPUOpsPerSecond[algoForShape(j.Shape)]; ok {
					mm.OpsPerSecond.V = rate
				}
			}
			subs = append(subs, &plan.Substrate{Name: m.Name, Zone: m.Zone, Model: &mm, BusyUntil: free})
			idx[m.Name] = m
		}
		env := &plan.Env{Substrates: subs, Grid: cfg.Grid, Learn: learnStore, Now: j.Arrival, PUE: cfg.PUE,
			InterZoneJPerByte: cfg.InterZoneJPerByte, IntraZoneJPerByte: cfg.IntraZoneJPerByte, UseActuals: cfg.Policy.UseActuals}
		d, _ := plan.Plan(env, &r)
		o := Outcome{Job: j.ID, Instance: j.Instance, Quality0: r.Quality}
		if d == nil || d.Chosen == nil {
			if d != nil && len(d.Reasons) > 0 {
				o.Reason = d.Reasons[len(d.Reasons)-1]
			}
			out = append(out, o)
			continue
		}
		c := d.Chosen
		m := idx[c.Substrate]
		o.Placed, o.Physics, o.Zone, o.Algorithm, o.Start = true, c.Physics, c.Zone, c.Algorithm, c.Start
		o.Wait = c.Start.Sub(j.Arrival)
		o.PredJ = c.Joules
		// Ground truth: the true model's cost with noise, and a quality
		// drawn from what calibration observed for this exact key.
		a, _ := plan.Find(c.Algorithm)
		var eff plan.Effort
		for _, e := range a.Efforts {
			if e.Name == c.Effort {
				eff = e
			}
		}
		ops, _ := plan.EstimateOps(a, eff, j.Shape)
		tm := *m.True
		if tm.Physics == device.CPU {
			if rate, ok := cfg.Cal.CPUOpsPerSecond[algoForShape(j.Shape)]; ok {
				tm.OpsPerSecond.V = rate
			}
		}
		cost := tm.Predict(ops)
		f := math.Exp(cfg.Noise * rng.NormFloat64())
		secs, joules := cost.Seconds*f, cost.Joules*f*cfg.PUE
		if r.DataBytes > 0 {
			if r.DataZone != "" && r.DataZone != c.Zone {
				joules += r.DataBytes * cfg.InterZoneJPerByte
			} else {
				joules += r.DataBytes * cfg.IntraZoneJPerByte
			}
		}
		dur := time.Duration(secs * float64(time.Second))
		series, _ := cfg.Grid.Zone(c.Zone)
		ci := series.CarbonActual(c.Start, dur)
		price := series.PriceAt(c.Start, dur)
		kwh := joules / 3.6e6
		o.Seconds, o.Joules, o.GCO2 = secs, joules, kwh*ci
		o.EUR = math.NaN()
		if !math.IsNaN(price) {
			o.EUR = kwh / 1000 * price
		}
		key := learn.Key{Class: string(j.Shape.Class), Algo: c.Algorithm, Effort: c.Effort, Physics: string(c.Physics), SizeBucket: learn.Bucket(j.Shape.Size())}
		qs := store.Samples(key)
		if len(qs) > 0 {
			o.Quality = qs[rng.IntN(len(qs))]
		}
		finish := c.Start.Add(dur)
		o.MissDL = finish.After(r.Deadline)
		o.MissQ = o.Quality < r.Quality-1e-12
		// Occupy the earliest free slot from the start to the finish.
		k := 0
		for i := range m.busy {
			if m.busy[i].Before(m.busy[k]) {
				k = i
			}
		}
		m.busy[k] = finish
		if cfg.Policy.Learn {
			learnStore.Measure(m.Name, "joules", joules/cfg.PUE, c.Joules/cfg.PUE)
			learnStore.Measure(m.Name, "seconds", secs, c.Seconds)
		}
		out = append(out, o)
	}
	return out
}

func algoForShape(s plan.Shape) string {
	if s.Class == plan.Classify {
		return "mac"
	}
	return "spin"
}

// Summary aggregates outcomes.
type Summary struct {
	Policy               string
	Jobs, Placed         int
	KWh, KgCO2, EUR      float64
	EURUnknown           int
	DeadlineMiss         float64
	QualityMiss          float64
	MeanWaitMin          float64
	PhysicsShare         map[device.Physics]float64
	ZoneShare            map[string]float64
	PredictionErrorFirst float64 // mean |pred-actual|/actual, first 10% of jobs
	PredictionErrorLast  float64 // last 10%
}

// Summarise computes the totals of one run.
func Summarise(name string, os []Outcome) Summary {
	s := Summary{Policy: name, Jobs: len(os), PhysicsShare: map[device.Physics]float64{}, ZoneShare: map[string]float64{}}
	var wait float64
	var errs []float64
	for _, o := range os {
		if !o.Placed {
			continue
		}
		s.Placed++
		s.KWh += o.Joules / 3.6e6
		s.KgCO2 += o.GCO2 / 1000
		if math.IsNaN(o.EUR) {
			s.EURUnknown++
		} else {
			s.EUR += o.EUR
		}
		if o.MissDL {
			s.DeadlineMiss++
		}
		if o.MissQ {
			s.QualityMiss++
		}
		wait += o.Wait.Minutes()
		s.PhysicsShare[o.Physics]++
		s.ZoneShare[o.Zone]++
		if o.Joules > 0 {
			errs = append(errs, math.Abs(o.PredJ-o.Joules)/o.Joules)
		}
	}
	if s.Placed > 0 {
		p := float64(s.Placed)
		s.DeadlineMiss /= p
		s.QualityMiss /= p
		s.MeanWaitMin = wait / p
		for k := range s.PhysicsShare {
			s.PhysicsShare[k] /= p
		}
		for k := range s.ZoneShare {
			s.ZoneShare[k] /= p
		}
	}
	if n := len(errs) / 10; n > 0 {
		s.PredictionErrorFirst = mean(errs[:n])
		s.PredictionErrorLast = mean(errs[len(errs)-n:])
	}
	return s
}

func mean(x []float64) float64 {
	var t float64
	for _, v := range x {
		t += v
	}
	return t / float64(len(x))
}

// Quantiles of a sample, for confidence intervals across seeds.
func Quantiles(x []float64, qs ...float64) []float64 {
	y := append([]float64(nil), x...)
	sort.Float64s(y)
	out := make([]float64, len(qs))
	for i, q := range qs {
		if len(y) == 0 {
			out[i] = math.NaN()
			continue
		}
		k := q * float64(len(y)-1)
		lo := int(math.Floor(k))
		hi := int(math.Ceil(k))
		out[i] = y[lo] + (y[hi]-y[lo])*(k-float64(lo))
	}
	return out
}

var _ = solve.Ops{}
