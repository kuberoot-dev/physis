package plan

import (
	"fmt"
	"math"
	"slices"
	"testing"
	"time"

	"github.com/kuberoot-dev/physis/pkg/device"
	"github.com/kuberoot-dev/physis/pkg/grid"
	"github.com/kuberoot-dev/physis/pkg/learn"
	"github.com/kuberoot-dev/physis/pkg/reason"
)

var t0 = time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)

// A grid that is dirty for the first 12 hours and clean afterwards; the
// forecast is the same as the actual unless a test says otherwise.
func twoPhaseGrid(zone string, dirty, clean float64) *grid.Series {
	n := 96 * 2
	s := &grid.Series{Zone: zone, Start: t0, Step: 15 * time.Minute, CI: make([]float64, n), CIForecast: make([]float64, n), Price: make([]float64, n)}
	for i := 0; i < n; i++ {
		v := clean
		if i < 48 {
			v = dirty
		}
		s.CI[i], s.CIForecast[i], s.Price[i] = v, v, v/5
	}
	return s
}

func knownQuality(st *learn.Store, shape Shape) {
	b := learn.Bucket(shape.Size())
	for _, a := range Catalog {
		if a.Class != shape.Class {
			continue
		}
		for _, p := range a.Physics {
			for _, e := range a.Efforts {
				k := learn.Key{Class: string(shape.Class), Algo: a.Name, Effort: e.Name, Physics: string(p), SizeBucket: b}
				for i := 0; i < 50; i++ {
					st.Observe(k, 1)
				}
			}
		}
	}
}

func env(zones map[string]*grid.Series, subs ...*Substrate) *Env {
	return &Env{Substrates: subs, Grid: &grid.Provider{Zones: zones}, Learn: learn.NewStore(), Now: t0, InterZoneJPerByte: 1e-6}
}

var shape = Shape{Class: MaxCut, N: 800, DegreeSum: 2 * 19176}

func TestDeferrableWorkWaitsForCleanHoursAndUrgentWorkDoesNot(t *testing.T) {
	e := env(map[string]*grid.Series{"DE": twoPhaseGrid("DE", 400, 50)},
		&Substrate{Name: "cpu", Zone: "DE", Model: device.Builtin(device.CPU)})
	knownQuality(e.Learn, shape)
	r := &Request{ID: "a", Shape: shape, Quality: 0.99, Deadline: t0.Add(20 * time.Hour), Deferrable: true, Policy: MinCarbon}
	d, err := Plan(e, r)
	if err != nil || d.Chosen == nil {
		t.Fatalf("no plan: %v %+v", err, d)
	}
	if d.Chosen.Start.Before(t0.Add(12 * time.Hour)) {
		t.Fatalf("deferrable min-carbon work started at %v, in the dirty phase", d.Chosen.Start)
	}
	r.Deferrable = false
	d, _ = Plan(e, r)
	if !d.Chosen.Start.Equal(t0) {
		t.Fatalf("urgent work delayed to %v", d.Chosen.Start)
	}
}

func TestDeadlineIsNeverTradedForCarbon(t *testing.T) {
	e := env(map[string]*grid.Series{"DE": twoPhaseGrid("DE", 400, 50)},
		&Substrate{Name: "cpu", Zone: "DE", Model: device.Builtin(device.CPU)})
	knownQuality(e.Learn, shape)
	r := &Request{ID: "a", Shape: shape, Quality: 0.99, Deadline: t0.Add(2 * time.Hour), Deferrable: true, Policy: MinCarbon}
	d, _ := Plan(e, r)
	if d.Chosen == nil || d.Chosen.Finish().After(r.Deadline) {
		t.Fatalf("plan misses deadline: %+v", d.Chosen)
	}
}

func TestUnmeasuredQualityIsNotPromised(t *testing.T) {
	e := env(map[string]*grid.Series{"DE": twoPhaseGrid("DE", 400, 50)},
		&Substrate{Name: "cpu", Zone: "DE", Model: device.Builtin(device.CPU)})
	r := &Request{ID: "a", Shape: shape, Quality: 0.99, Deadline: t0.Add(2 * time.Hour), Policy: MinCarbon}
	d, _ := Plan(e, r)
	if d.Chosen != nil {
		t.Fatalf("planned with no quality evidence: %+v", d.Chosen)
	}
	if len(d.Reasons) == 0 {
		t.Fatal("rejection without a reason")
	}
}

func TestZonesAreHardConstraints(t *testing.T) {
	e := env(map[string]*grid.Series{"DE": twoPhaseGrid("DE", 400, 400), "FR": twoPhaseGrid("FR", 20, 20)},
		&Substrate{Name: "cpu-de", Zone: "DE", Model: device.Builtin(device.CPU)},
		&Substrate{Name: "cpu-fr", Zone: "FR", Model: device.Builtin(device.CPU)})
	knownQuality(e.Learn, shape)
	r := &Request{ID: "a", Shape: shape, Quality: 0.99, Deadline: t0.Add(time.Hour), Policy: MinCarbon}
	d, _ := Plan(e, r)
	if d.Chosen.Zone != "FR" {
		t.Fatalf("min-carbon chose %s over cleaner FR", d.Chosen.Zone)
	}
	r.Zones = []string{"DE"}
	d, _ = Plan(e, r)
	if d.Chosen == nil || d.Chosen.Zone != "DE" {
		t.Fatalf("zone restriction ignored: %+v", d.Chosen)
	}
}

func TestBudgetRejectsAndParetoIsNonDominated(t *testing.T) {
	e := env(map[string]*grid.Series{"DE": twoPhaseGrid("DE", 400, 50)},
		&Substrate{Name: "cpu", Zone: "DE", Model: device.Builtin(device.CPU)},
		&Substrate{Name: "ising", Zone: "DE", Model: device.Builtin(device.IsingDigital)})
	knownQuality(e.Learn, shape)
	r := &Request{ID: "a", Shape: shape, Quality: 0.99, Deadline: t0.Add(20 * time.Hour), Deferrable: true, Policy: MinEnergy}
	d, _ := Plan(e, r)
	for _, a := range d.Pareto {
		for _, b := range d.Pareto {
			if Dominates(b, a) {
				t.Fatalf("pareto member dominated: %+v by %+v", a, b)
			}
		}
	}
	r.Budget.Joules = d.Chosen.Joules / 2
	d2, _ := Plan(e, r)
	if d2.Chosen != nil && d2.Chosen.Joules > r.Budget.Joules {
		t.Fatalf("budget exceeded: %v > %v", d2.Chosen.Joules, r.Budget.Joules)
	}
}

func TestCalibrationWindowsAreAvoided(t *testing.T) {
	m := device.Builtin(device.AnalogIMC)
	// A 10-second run asked to start 5 seconds into a calibration window
	// waits until the window ends.
	got := m.AvailableAt(5, 10)
	if got != m.CalDuration {
		t.Fatalf("available at %v, want %v", got, m.CalDuration)
	}
	// A run that would cross the next window is moved after it.
	got = m.AvailableAt(m.CalEvery-5, 10)
	if math.Abs(got-(m.CalEvery+m.CalDuration)) > 1e-9 {
		t.Fatalf("available at %v, want after the next window", got)
	}
}

// The oracle sees actual intensity; the scheduler sees only the forecast.
func TestSchedulerUsesForecastNotActual(t *testing.T) {
	s := twoPhaseGrid("DE", 400, 50)
	// The forecast wrongly says the night stays dirty.
	for i := 48; i < len(s.CIForecast); i++ {
		s.CIForecast[i] = 500
	}
	e := env(map[string]*grid.Series{"DE": s}, &Substrate{Name: "cpu", Zone: "DE", Model: device.Builtin(device.CPU)})
	knownQuality(e.Learn, shape)
	r := &Request{ID: "a", Shape: shape, Quality: 0.99, Deadline: t0.Add(20 * time.Hour), Deferrable: true, Policy: MinCarbon}
	d, _ := Plan(e, r)
	if d.Chosen.Start.After(t0.Add(12 * time.Hour)) {
		t.Fatalf("scheduler acted on information it could not have: start %v", d.Chosen.Start)
	}
	e.UseActuals = true
	d, _ = Plan(e, r)
	if d.Chosen.Start.Before(t0.Add(12 * time.Hour)) {
		t.Fatalf("oracle did not use actuals: start %v", d.Chosen.Start)
	}
}

// Deferred work reserves only its own interval: a substrate reserved for a
// run tomorrow is free now.
func TestReservationsBlockOnlyTheirInterval(t *testing.T) {
	sub := &Substrate{Name: "ising", Zone: "DE", Model: device.Builtin(device.IsingDigital), Slots: 1,
		Reserved: []Interval{{From: t0.Add(10 * time.Hour), To: t0.Add(11 * time.Hour)}}}
	e := env(map[string]*grid.Series{"DE": twoPhaseGrid("DE", 400, 400)}, sub)
	knownQuality(e.Learn, shape)
	r := &Request{ID: "a", Shape: shape, Quality: 0.99, Deadline: t0.Add(time.Hour), Policy: Fastest, Pin: &PinAlgorithm{Physics: []device.Physics{device.IsingDigital}}}
	d, _ := Plan(e, r)
	if d.Chosen == nil || !d.Chosen.Start.Equal(t0) {
		t.Fatalf("a reservation ten hours away delayed work now: %+v", d.Chosen)
	}
	sub.Reserved = []Interval{{From: t0, To: t0.Add(30 * time.Minute)}}
	d, _ = Plan(e, r)
	if d.Chosen == nil || d.Chosen.Start.Before(t0.Add(30*time.Minute)) {
		t.Fatalf("overlapping reservation ignored: %+v", d.Chosen)
	}
}

// Links are objects with a price: moving data costs their energy and time,
// zones without a link cannot exchange data, and a path through a
// jurisdiction the request does not allow is not taken.
func TestLinksPriceAndConstrainDataMovement(t *testing.T) {
	zones := map[string]*grid.Series{"DE": twoPhaseGrid("DE", 400, 400), "FR": twoPhaseGrid("FR", 20, 20), "GB": twoPhaseGrid("GB", 100, 100)}
	e := env(zones,
		&Substrate{Name: "cpu-fr", Zone: "FR", Jurisdiction: "EU", Model: device.Builtin(device.CPU)},
		&Substrate{Name: "cpu-gb", Zone: "GB", Jurisdiction: "UK", Model: device.Builtin(device.CPU)})
	e.Links = []Link{
		{Name: "de-fr", From: "DE", To: "FR", JPerByte: 1e-6, BytesPerSecond: 1e8, Jurisdictions: []string{"EU"}},
		{Name: "gb-fr", From: "GB", To: "FR", JPerByte: 1e-6, BytesPerSecond: 1e8, Jurisdictions: []string{"UK", "EU"}},
	}
	knownQuality(e.Learn, shape)
	r := &Request{ID: "a", Shape: shape, Quality: 0.99, Deadline: t0.Add(time.Hour), Policy: MinCarbon, DataZone: "DE", DataBytes: 1e9}
	d, _ := Plan(e, r)
	if d.Chosen == nil || d.Chosen.Zone != "FR" || d.Chosen.Link != "de-fr" {
		t.Fatalf("expected the DE data to move to FR over de-fr: %+v", d.Chosen)
	}
	if math.Abs(d.Chosen.MoveJoules-1e3) > 1e-6 || d.Chosen.MoveSeconds < 10 || d.Chosen.Start.Before(t0.Add(10*time.Second)) {
		t.Fatalf("move not priced from the link: %+v", d.Chosen)
	}
	// The move is charged at the mean intensity of its two ends (400 and 20
	// g/kWh), the computation at the intensity where it runs.
	c := d.Chosen
	wantG := (c.Joules-c.MoveJoules)/3.6e6*20 + c.MoveJoules/3.6e6*(400+20)/2
	if math.Abs(c.GCO2-wantG) > 1e-9*math.Max(1, wantG) {
		t.Fatalf("move carbon not at the path's mean intensity: got %g g, want %g g", c.GCO2, wantG)
	}
	// No DE-GB link: GB is unreachable for DE data.
	r.Zones = []string{"GB"}
	if d, _ = Plan(e, r); d.Chosen != nil {
		t.Fatalf("planned over a link that does not exist: %+v", d.Chosen)
	}
	// GB data may go to FR only through UK and EU; restricted to EU, it cannot.
	r.Zones, r.DataZone, r.Jurisdictions = nil, "GB", []string{"EU"}
	if d, _ = Plan(e, r); d.Chosen != nil {
		t.Fatalf("data crossed a disallowed jurisdiction: %+v", d.Chosen)
	}
}

func TestRejectionsCarryStableCodes(t *testing.T) {
	e := env(map[string]*grid.Series{"DE": twoPhaseGrid("DE", 400, 50)},
		&Substrate{Name: "cpu-fr", Zone: "FR", Jurisdiction: "EU", Model: device.Builtin(device.CPU)},
		&Substrate{Name: "cpu-us", Zone: "DE", Jurisdiction: "US", Model: device.Builtin(device.CPU)},
		&Substrate{Name: "cpu-de", Zone: "DE", Jurisdiction: "EU", Model: device.Builtin(device.CPU)})
	r := &Request{ID: "a", Shape: shape, Quality: 0.99, Deadline: t0.Add(2 * time.Hour), Policy: MinCarbon,
		Zones: []string{"DE"}, Jurisdictions: []string{"EU"}}
	d, _ := Plan(e, r)
	if d.Chosen != nil {
		t.Fatalf("planned with no quality evidence: %+v", d.Chosen)
	}
	want := []string{reason.ZoneNotAllowed, reason.JurisdictionNotAllowed, reason.QualityUnreachable}
	if fmt.Sprint(d.ReasonCodes) != fmt.Sprint(want) {
		t.Fatalf("codes %v, want %v", d.ReasonCodes, want)
	}
	knownQuality(e.Learn, shape)
	r.Budget.Joules = 1e-12
	d, _ = Plan(e, r)
	if d.Chosen != nil || !slices.Contains(d.ReasonCodes, reason.BudgetExceeded) {
		t.Fatalf("a budget no plan meets: chosen %+v, codes %v", d.Chosen, d.ReasonCodes)
	}
	r.Budget.Joules = 0
	r.Deadline = t0.Add(time.Nanosecond)
	d, _ = Plan(e, r)
	if d.Chosen != nil || !slices.Contains(d.ReasonCodes, reason.DeadlineTooShort) {
		t.Fatalf("an impossible deadline: chosen %+v, codes %v", d.Chosen, d.ReasonCodes)
	}
}
