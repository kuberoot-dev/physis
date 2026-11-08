package grid

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"time"
)

// LifecycleFactor is the life-cycle emission factor of a generation type in
// gCO2e/kWh: the medians of IPCC AR5 WGIII Annex III, Table A.III.2
// (ref:ipcc2014). Types the table does not cover are marked as assumptions in
// FactorSource.
var LifecycleFactor = map[string]float64{
	"Fossil brown coal / lignite": 820,
	"Fossil hard coal":            820,
	"Fossil coal-derived gas":     820,
	"Fossil gas":                  490,
	"Fossil oil":                  650,
	"Biomass":                     230,
	"Waste":                       500,
	"Others":                      500,
	"Geothermal":                  38,
	"Hydro Run-of-River":          24,
	"Hydro water reservoir":       24,
	"Nuclear":                     12,
	"Solar":                       48,
	"Wind onshore":                11.5,
	"Wind offshore":               12,
}

// FactorSource says where each factor comes from.
var FactorSource = map[string]string{
	"Fossil oil": "assumption (between gas and coal; not in Table A.III.2)",
	"Waste":      "assumption",
	"Others":     "assumption",
	"Fossil coal-derived gas": "assumption (treated as coal)",
	"Fossil brown coal / lignite": "ref:ipcc2014 (coal median; lignite is higher, so this understates)",
}

// Generation types that are not generation, or whose carbon belongs to
// another time or place, are left out of the production-based intensity:
// storage shifts energy already counted when it was charged, trade carries
// another zone's mix.
var excluded = map[string]bool{
	"Hydro pumped storage consumption": true, "Hydro pumped storage": true,
	"Cross border electricity trading": true, "Load": true, "Residual load": true,
	"Renewable share of load": true, "Renewable share of generation": true,
	"Battery Storage (Discharge)": true, "Battery Storage (Charge)": true,
}

type ecPower struct {
	Unix  []int64 `json:"unix_seconds"`
	Types []struct {
		Name string     `json:"name"`
		Data []*float64 `json:"data"`
	} `json:"production_types"`
}

type ecPrice struct {
	Unix  []int64    `json:"unix_seconds"`
	Price []*float64 `json:"price"`
}

func getJSON(c *http.Client, url string, v any) error {
	for attempt := 0; ; attempt++ {
		resp, err := c.Get(url)
		if err == nil && resp.StatusCode == 200 {
			defer resp.Body.Close()
			return json.NewDecoder(resp.Body).Decode(v)
		}
		if resp != nil {
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
		}
		if attempt == 4 {
			if err == nil {
				err = fmt.Errorf("status %d", resp.StatusCode)
			}
			return fmt.Errorf("%s: %w", url, err)
		}
		time.Sleep(time.Duration(attempt+1) * 3 * time.Second)
	}
}

// FetchEnergyCharts builds a 15-minute series for a country from the
// Energy-Charts API (Fraunhofer ISE): carbon intensity computed from the
// generation mix with LifecycleFactor, a persistence forecast (the same
// quarter-hour one day earlier, which is what was known a day ahead), and the
// day-ahead price of the bidding zone.
func FetchEnergyCharts(c *http.Client, zone, country, bzn string, from, to time.Time) (*Series, error) {
	step := 15 * time.Minute
	n := int(to.Sub(from) / step)
	s := &Series{Zone: zone, Start: from, Step: step, CI: nan(n), CIForecast: nan(n), Price: nan(n)}
	// One extra day before the period feeds the first day's forecast.
	ext := from.Add(-24 * time.Hour)
	ci := map[int64]float64{}
	for m := ext; m.Before(to); m = m.AddDate(0, 0, 14) {
		end := m.AddDate(0, 0, 14)
		if end.After(to) {
			end = to
		}
		var p ecPower
		url := fmt.Sprintf("https://api.energy-charts.info/public_power?country=%s&start=%s&end=%s", country, m.Format(time.RFC3339), end.Format(time.RFC3339))
		if err := getJSON(c, url, &p); err != nil {
			return nil, err
		}
		for i, t := range p.Unix {
			var gen, co2 float64
			for _, ty := range p.Types {
				if excluded[ty.Name] || i >= len(ty.Data) || ty.Data[i] == nil || *ty.Data[i] <= 0 {
					continue
				}
				f, ok := LifecycleFactor[ty.Name]
				if !ok {
					f = 500 // unknown type: counted, conservatively, as "Others"
				}
				gen += *ty.Data[i]
				co2 += *ty.Data[i] * f
			}
			if gen > 0 {
				ci[t] = co2 / gen
			}
		}
		var pr ecPrice
		url = fmt.Sprintf("https://api.energy-charts.info/price?bzn=%s&start=%s&end=%s", bzn, m.Format(time.RFC3339), end.Format(time.RFC3339))
		if err := getJSON(c, url, &pr); err != nil {
			return nil, err
		}
		for i, t := range pr.Unix {
			if pr.Price[i] == nil {
				continue
			}
			// Hourly prices fill their four quarter-hours.
			span := int64(900)
			if i+1 < len(pr.Unix) {
				span = pr.Unix[i+1] - t
			}
			for q := t; q < t+span; q += 900 {
				if k := int((time.Unix(q, 0).Sub(from)) / step); k >= 0 && k < n {
					s.Price[k] = *pr.Price[i]
				}
			}
		}
	}
	for k := 0; k < n; k++ {
		t := from.Add(time.Duration(k) * step).Unix()
		if v, ok := ci[t]; ok {
			s.CI[k] = v
		}
		if v, ok := ci[t-86400]; ok {
			s.CIForecast[k] = v
		}
	}
	fill(s.CI)
	fill(s.CIForecast)
	return s, nil
}

type ukResp struct {
	Data []struct {
		From      string `json:"from"`
		Intensity struct {
			Forecast *float64 `json:"forecast"`
			Actual   *float64 `json:"actual"`
		} `json:"intensity"`
	} `json:"data"`
}

// FetchGB builds a series for Great Britain from the National Energy System
// Operator's Carbon Intensity API, which publishes its own forecast next to
// the actual. Half-hours fill two quarter-hours; there is no price.
func FetchGB(c *http.Client, from, to time.Time) (*Series, error) {
	step := 15 * time.Minute
	n := int(to.Sub(from) / step)
	s := &Series{Zone: "GB", Start: from, Step: step, CI: nan(n), CIForecast: nan(n), Price: nan(n)}
	for m := from; m.Before(to); m = m.AddDate(0, 0, 13) {
		end := m.AddDate(0, 0, 13)
		if end.After(to) {
			end = to
		}
		var r ukResp
		url := fmt.Sprintf("https://api.carbonintensity.org.uk/intensity/%s/%s", m.Format("2006-01-02T15:04Z"), end.Format("2006-01-02T15:04Z"))
		if err := getJSON(c, url, &r); err != nil {
			return nil, err
		}
		for _, d := range r.Data {
			t, err := time.Parse("2006-01-02T15:04Z", d.From)
			if err != nil {
				continue
			}
			for _, q := range []time.Time{t, t.Add(step)} {
				k := int(q.Sub(from) / step)
				if k < 0 || k >= n {
					continue
				}
				if d.Intensity.Actual != nil {
					s.CI[k] = *d.Intensity.Actual
				}
				if d.Intensity.Forecast != nil {
					s.CIForecast[k] = *d.Intensity.Forecast
				}
			}
		}
	}
	fill(s.CI)
	fill(s.CIForecast)
	return s, nil
}

func nan(n int) []float64 {
	v := make([]float64, n)
	for i := range v {
		v[i] = math.NaN()
	}
	return v
}

// fill carries the last known value over gaps (and the first known value
// backwards), so a missing quarter-hour does not read as zero carbon.
func fill(v []float64) {
	last := math.NaN()
	for i := range v {
		if math.IsNaN(v[i]) {
			v[i] = last
		} else {
			last = v[i]
		}
	}
	first := math.NaN()
	for i := len(v) - 1; i >= 0; i-- {
		if math.IsNaN(v[i]) {
			v[i] = first
		} else {
			first = v[i]
		}
	}
}
