// Package grid holds time series of the electricity grid a substrate draws
// from: carbon intensity (gCO2e/kWh) and wholesale price (EUR/MWh), both as
// what actually happened and as what was known in advance. The scheduler only
// ever sees the forecast; experiments are scored against the actuals. Mixing
// the two would let a scheduler look into the future and flatter itself.
package grid

import (
	"encoding/csv"
	"fmt"
	"io"
	"math"
	"os"
	"sort"
	"strconv"
	"time"
)

// Series is one zone's grid signal on a fixed step.
type Series struct {
	Zone  string
	Start time.Time
	Step  time.Duration
	// Carbon intensity, gCO2e/kWh.
	CI         []float64
	CIForecast []float64
	// Day-ahead price, EUR/MWh; NaN where unknown. The day-ahead price is
	// fixed the day before delivery, so it is its own forecast.
	Price []float64
}

func (s *Series) End() time.Time { return s.Start.Add(time.Duration(len(s.CI)) * s.Step) }

func (s *Series) index(t time.Time) int {
	i := int(t.Sub(s.Start) / s.Step)
	if i < 0 {
		return 0
	}
	if i >= len(s.CI) {
		return len(s.CI) - 1
	}
	return i
}

// Window averages a signal over [from, from+d), weighting partial steps.
func (s *Series) window(v []float64, from time.Time, d time.Duration) float64 {
	if d <= 0 {
		return v[s.index(from)]
	}
	end := from.Add(d)
	var sum, w float64
	for t := from; t.Before(end); {
		i := s.index(t)
		stepEnd := s.Start.Add(time.Duration(i+1) * s.Step)
		if stepEnd.After(end) || !stepEnd.After(t) {
			stepEnd = end
		}
		dt := stepEnd.Sub(t).Seconds()
		if x := v[i]; !math.IsNaN(x) {
			sum += x * dt
			w += dt
		}
		t = stepEnd
	}
	if w == 0 {
		return math.NaN()
	}
	return sum / w
}

// CarbonActual is the actual mean intensity over a run.
func (s *Series) CarbonActual(from time.Time, d time.Duration) float64 {
	return s.window(s.CI, from, d)
}

// CarbonForecast is the intensity a scheduler could know at time "now" for a
// run starting at "from". Forecasts in the past collapse to actuals.
func (s *Series) CarbonForecast(now, from time.Time, d time.Duration) float64 {
	if from.Before(now) {
		// Already running: the past part is known, the rest is forecast.
		return s.CarbonActual(from, d)
	}
	return s.window(s.CIForecast, from, d)
}

// PriceAt is the mean day-ahead price over a run.
func (s *Series) PriceAt(from time.Time, d time.Duration) float64 {
	return s.window(s.Price, from, d)
}

// Provider gives the scheduler forecasts and the evaluator actuals.
type Provider struct {
	Zones map[string]*Series
}

func (p *Provider) Zone(z string) (*Series, error) {
	s, ok := p.Zones[z]
	if !ok {
		return nil, fmt.Errorf("no grid data for zone %q", z)
	}
	return s, nil
}

// Names lists zones in a stable order.
func (p *Provider) Names() []string {
	var n []string
	for z := range p.Zones {
		n = append(n, z)
	}
	sort.Strings(n)
	return n
}

// WriteCSV stores a series as "unix,ci,ci_forecast,price".
func (s *Series) WriteCSV(w io.Writer) error {
	cw := csv.NewWriter(w)
	if err := cw.Write([]string{"unix", "ci_g_per_kwh", "ci_forecast_g_per_kwh", "price_eur_per_mwh"}); err != nil {
		return err
	}
	f := func(x float64) string {
		if math.IsNaN(x) {
			return ""
		}
		return strconv.FormatFloat(x, 'f', 2, 64)
	}
	for i := range s.CI {
		t := s.Start.Add(time.Duration(i) * s.Step).Unix()
		if err := cw.Write([]string{strconv.FormatInt(t, 10), f(s.CI[i]), f(s.CIForecast[i]), f(s.Price[i])}); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}

// ReadCSV loads a series written by WriteCSV.
func ReadCSV(zone string, r io.Reader) (*Series, error) {
	rows, err := csv.NewReader(r).ReadAll()
	if err != nil {
		return nil, err
	}
	if len(rows) < 3 {
		return nil, fmt.Errorf("%s: too few rows", zone)
	}
	parse := func(x string) float64 {
		if x == "" {
			return math.NaN()
		}
		v, err := strconv.ParseFloat(x, 64)
		if err != nil {
			return math.NaN()
		}
		return v
	}
	s := &Series{Zone: zone}
	var t0, t1 int64
	for i, row := range rows[1:] {
		t, err := strconv.ParseInt(row[0], 10, 64)
		if err != nil {
			return nil, err
		}
		if i == 0 {
			t0 = t
		}
		if i == 1 {
			t1 = t
		}
		s.CI = append(s.CI, parse(row[1]))
		s.CIForecast = append(s.CIForecast, parse(row[2]))
		s.Price = append(s.Price, parse(row[3]))
	}
	s.Start = time.Unix(t0, 0).UTC()
	s.Step = time.Duration(t1-t0) * time.Second
	return s, nil
}

// LoadDir reads every <zone>.csv in a directory.
func LoadDir(dir string, zones []string) (*Provider, error) {
	p := &Provider{Zones: map[string]*Series{}}
	for _, z := range zones {
		f, err := os.Open(dir + "/" + z + ".csv")
		if err != nil {
			return nil, err
		}
		s, err := ReadCSV(z, f)
		f.Close()
		if err != nil {
			return nil, err
		}
		p.Zones[z] = s
	}
	return p, nil
}
