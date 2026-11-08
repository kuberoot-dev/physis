// Package plan is the physis scheduler. Given a problem stated as an outcome
// (quality, deadline, budget) rather than as resources, it enumerates every
// way to obtain it -- algorithm x effort x physics x place x start time --
// predicts each one's time, energy, carbon and money from device models,
// learned quality distributions and grid forecasts, drops what violates a
// constraint, and picks by policy. It also returns the Pareto front, so a
// human or an agent can see what the choice traded away.
package plan

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/kuberoot-dev/physis/pkg/device"
	"github.com/kuberoot-dev/physis/pkg/grid"
	"github.com/kuberoot-dev/physis/pkg/learn"
)

// Policy is what the requester optimises.
type Policy string

const (
	MinCarbon Policy = "min-carbon"
	MinCost   Policy = "min-cost"
	MinEnergy Policy = "min-energy"
	Fastest   Policy = "fastest"
)

// Budget caps a run; zero means no cap.
type Budget struct {
	Joules float64 `json:"joules,omitempty"`
	EUR    float64 `json:"eur,omitempty"`
	GCO2   float64 `json:"gco2,omitempty"`
}

// Request is a problem stated as an outcome.
type Request struct {
	ID    string `json:"id"`
	Shape Shape  `json:"shape"`
	// Quality is the target: cut / reference cut, or classification
	// accuracy / exact accuracy.
	Quality float64 `json:"quality"`
	// Confidence is the required lower bound on P(quality >= target).
	Confidence float64   `json:"confidence"`
	Deadline   time.Time `json:"deadline"`
	Deferrable bool      `json:"deferrable"`
	Policy     Policy    `json:"policy"`
	Budget     Budget    `json:"budget"`
	Zones      []string  `json:"zones,omitempty"` // allowed zones; empty = any
	// Jurisdictions under which the operator of a substrate may fall;
	// empty = any. Sovereignty is a constraint, not a preference (P11).
	Jurisdictions []string      `json:"jurisdictions,omitempty"`
	DataZone      string        `json:"dataZone,omitempty"`
	DataBytes     float64       `json:"dataBytes,omitempty"`
	Pin           *PinAlgorithm `json:"pin,omitempty"` // fix algorithm/physics (baselines, tests)
}

// PinAlgorithm restricts the search.
type PinAlgorithm struct {
	Physics []device.Physics
	NoDefer bool
}

// Substrate is one schedulable device.
type Substrate struct {
	Name         string        `json:"name"`
	Zone         string        `json:"zone"`
	Operator     string        `json:"operator,omitempty"`
	Jurisdiction string        `json:"jurisdiction,omitempty"`
	Model        *device.Model `json:"-"`
	BusyUntil    time.Time     `json:"busyUntil"`
}

// Env is everything the scheduler consults.
type Env struct {
	Substrates []*Substrate
	Grid       *grid.Provider
	Learn      *learn.Store
	// Now is the decision time; forecasts are as known at Now.
	Now time.Time
	// Slot is the granularity of start times for deferrable work.
	Slot time.Duration
	// PUE multiplies device energy by facility overhead.
	PUE float64
	// TransferJPerByte between zones and within one.
	InterZoneJPerByte, IntraZoneJPerByte float64
	// UseActuals makes the scheduler see actual grid values instead of
	// forecasts: the oracle that bounds what forecasting costs.
	UseActuals bool
}

// Candidate is one way to run a request, with its predictions.
type Candidate struct {
	Substrate string         `json:"substrate"`
	Zone      string         `json:"zone"`
	Physics   device.Physics `json:"physics"`
	Algorithm string         `json:"algorithm"`
	Effort    string         `json:"effort"`
	Start     time.Time      `json:"start"`
	Seconds   float64        `json:"seconds"`
	Joules    float64        `json:"joules"`
	GCO2      float64        `json:"gco2"`
	EUR       float64        `json:"eur"`
	PQuality  float64        `json:"pQuality"`
	QualityN  int            `json:"qualitySamples"`
	// CarbonIntensity used, gCO2e/kWh.
	CI float64 `json:"ci"`
}

// Finish is when the run is predicted to end.
func (c Candidate) Finish() time.Time {
	return c.Start.Add(time.Duration(c.Seconds * float64(time.Second)))
}

// Decision is the outcome of planning.
type Decision struct {
	Chosen *Candidate  `json:"chosen,omitempty"`
	Pareto []Candidate `json:"pareto,omitempty"`
	Count  int         `json:"candidates"`
	// Physics lists every physics with at least one feasible candidate.
	Physics []device.Physics `json:"feasiblePhysics,omitempty"`
	Reasons []string         `json:"reasons,omitempty"` // why candidates were rejected (sample)
}

// zoneAllowed reports whether a zone is in the request's allow-list.
func zoneAllowed(r *Request, z string) bool {
	if len(r.Zones) == 0 {
		return true
	}
	for _, a := range r.Zones {
		if a == z {
			return true
		}
	}
	return false
}

func pinned(r *Request, p device.Physics) bool {
	if r.Pin == nil || len(r.Pin.Physics) == 0 {
		return true
	}
	for _, x := range r.Pin.Physics {
		if x == p {
			return true
		}
	}
	return false
}

// Plan enumerates, predicts, filters and chooses.
func Plan(env *Env, r *Request) (*Decision, error) {
	if env.Slot <= 0 {
		env.Slot = 15 * time.Minute
	}
	if env.PUE <= 0 {
		env.PUE = 1
	}
	conf := r.Confidence
	if conf <= 0 {
		conf = 0.9
	}
	d := &Decision{}
	reject := func(format string, a ...any) {
		if len(d.Reasons) < 20 {
			d.Reasons = append(d.Reasons, fmt.Sprintf(format, a...))
		}
	}
	var cands []Candidate
	for _, s := range env.Substrates {
		if !zoneAllowed(r, s.Zone) {
			reject("%s: zone %s not allowed", s.Name, s.Zone)
			continue
		}
		if len(r.Jurisdictions) > 0 && !contains(r.Jurisdictions, s.Jurisdiction) {
			reject("%s: operator jurisdiction %q not allowed", s.Name, s.Jurisdiction)
			continue
		}
		if !pinned(r, s.Model.Physics) {
			continue
		}
		series, err := env.Grid.Zone(s.Zone)
		if err != nil {
			reject("%s: %v", s.Name, err)
			continue
		}
		for _, a := range Catalog {
			if !CanRun(s.Model, a, r.Shape) {
				continue
			}
			for _, e := range a.Efforts {
				ops, ok := EstimateOps(a, e, r.Shape)
				if !ok {
					continue
				}
				key := learn.Key{Class: string(r.Shape.Class), Algo: a.Name, Effort: e.Name, Physics: string(s.Model.Physics), SizeBucket: learn.Bucket(r.Shape.Size())}
				pq, n := env.Learn.ProbAtLeast(key, r.Quality, 1.0)
				if pq < conf {
					reject("%s %s/%s: P(q>=%.3f) lower bound %.2f from %d runs < %.2f", s.Name, a.Name, e.Name, r.Quality, pq, n, conf)
					continue
				}
				cost := s.Model.Predict(ops)
				secs := cost.Seconds * env.Learn.Correct(s.Name, "seconds")
				joules := cost.Joules * env.Learn.Correct(s.Name, "joules") * env.PUE
				if r.DataBytes > 0 {
					if r.DataZone != "" && r.DataZone != s.Zone {
						joules += r.DataBytes * env.InterZoneJPerByte
					} else {
						joules += r.DataBytes * env.IntraZoneJPerByte
					}
				}
				dur := time.Duration(secs * float64(time.Second))
				earliest := env.Now
				if s.BusyUntil.After(earliest) {
					earliest = s.BusyUntil
				}
				starts := []time.Time{earliest}
				if r.Deferrable && (r.Pin == nil || !r.Pin.NoDefer) {
					first := earliest.Truncate(env.Slot)
					for t := first.Add(env.Slot); !t.Add(dur).After(r.Deadline); t = t.Add(env.Slot) {
						starts = append(starts, t)
					}
				}
				for _, st := range starts {
					avail := s.Model.AvailableAt(float64(st.Unix()), secs)
					st = time.Unix(int64(math.Ceil(avail)), 0).UTC()
					if st.Add(dur).After(r.Deadline) {
						reject("%s %s/%s: finishes after deadline", s.Name, a.Name, e.Name)
						continue
					}
					var ci float64
					if env.UseActuals {
						ci = series.CarbonActual(st, dur)
					} else {
						ci = series.CarbonForecast(env.Now, st, dur)
					}
					kwh := joules / 3.6e6
					price := series.PriceAt(st, dur)
					// Unknown price stays NaN: it cannot be minimised, and a
					// zone without a published price must not look free.
					eur := math.NaN()
					if finite(price) {
						eur = kwh / 1000 * price
					}
					c := Candidate{Substrate: s.Name, Zone: s.Zone, Physics: s.Model.Physics, Algorithm: a.Name, Effort: e.Name,
						Start: st, Seconds: secs, Joules: joules, GCO2: kwh * ci, EUR: eur, PQuality: pq, QualityN: n, CI: ci}
					if !withinBudget(r.Budget, c) {
						reject("%s %s/%s: over budget", s.Name, a.Name, e.Name)
						continue
					}
					cands = append(cands, c)
				}
			}
		}
	}
	d.Count = len(cands)
	seen := map[device.Physics]bool{}
	for _, c := range cands {
		if !seen[c.Physics] {
			seen[c.Physics] = true
			d.Physics = append(d.Physics, c.Physics)
		}
	}
	if len(cands) == 0 {
		return d, nil
	}
	best := choose(r.Policy, cands)
	d.Chosen = &best
	d.Pareto = pareto(cands)
	return d, nil
}

func withinBudget(b Budget, c Candidate) bool {
	if b.Joules > 0 && c.Joules > b.Joules {
		return false
	}
	if b.EUR > 0 && (math.IsNaN(c.EUR) || c.EUR > b.EUR) {
		return false
	}
	if b.GCO2 > 0 && c.GCO2 > b.GCO2 {
		return false
	}
	return true
}

// objective returns the quantity a policy minimises.
func objective(p Policy, c Candidate) float64 {
	switch p {
	case MinCarbon:
		return c.GCO2
	case MinCost:
		if math.IsNaN(c.EUR) {
			return math.Inf(1)
		}
		return c.EUR
	case MinEnergy:
		return c.Joules
	default:
		return float64(c.Finish().UnixNano())
	}
}

// choose picks the minimum of the objective. Values within 0.1% of each other
// count as equal, and equals go to the earlier finish, then to lower energy:
// waiting an hour to save a rounding error is not a saving.
func choose(p Policy, cs []Candidate) Candidate {
	better := func(c, b Candidate) bool {
		x, y := objective(p, c), objective(p, b)
		if p != Fastest && math.Abs(x-y) > math.Abs(y)*1e-3 {
			return x < y
		}
		if p == Fastest && x != y {
			return x < y
		}
		if !c.Finish().Equal(b.Finish()) {
			return c.Finish().Before(b.Finish())
		}
		return c.Joules < b.Joules
	}
	best := cs[0]
	for _, c := range cs[1:] {
		if better(c, best) {
			best = c
		}
	}
	return best
}

// pareto keeps candidates not dominated on (carbon, money, energy, finish).
// Where either price is unknown, money is not comparable and the other three
// axes decide.
func pareto(cs []Candidate) []Candidate {
	var out []Candidate
	for i, a := range cs {
		dominated := false
		for j, b := range cs {
			if i != j && Dominates(b, a) {
				dominated = true
				break
			}
		}
		if !dominated {
			out = append(out, a)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].GCO2 < out[j].GCO2 })
	if len(out) > 12 {
		out = out[:12]
	}
	return out
}

// Dominates reports whether b is at least as good as a on every comparable
// axis and strictly better on one.
func Dominates(b, a Candidate) bool {
	money := !math.IsNaN(a.EUR) && !math.IsNaN(b.EUR)
	if b.GCO2 > a.GCO2 || b.Joules > a.Joules || b.Finish().After(a.Finish()) || (money && b.EUR > a.EUR) {
		return false
	}
	return b.GCO2 < a.GCO2 || b.Joules < a.Joules || b.Finish().Before(a.Finish()) || (money && b.EUR < a.EUR)
}

func (c Candidate) MarshalJSON() ([]byte, error) {
	type alias Candidate
	var eur *float64
	if !math.IsNaN(c.EUR) {
		v := c.EUR
		eur = &v
	}
	return json.Marshal(struct {
		alias
		EUR *float64 `json:"eur"`
	}{alias(c), eur})
}

// UnmarshalJSON reads null back as NaN.
func (c *Candidate) UnmarshalJSON(b []byte) error {
	type alias Candidate
	aux := struct {
		*alias
		EUR *float64 `json:"eur"`
	}{alias: (*alias)(c)}
	if err := json.Unmarshal(b, &aux); err != nil {
		return err
	}
	c.EUR = math.NaN()
	if aux.EUR != nil {
		c.EUR = *aux.EUR
	}
	return nil
}

func contains(xs []string, x string) bool {
	for _, y := range xs {
		if y == x {
			return true
		}
	}
	return false
}
