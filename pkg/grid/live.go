package grid

import (
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// ZoneSource says how to fetch a zone live.
type ZoneSource struct {
	Zone, Country, BiddingZone string
}

// DefaultZones are the zones of the prototype.
var DefaultZones = []ZoneSource{{"DE", "de", "DE-LU"}, {"FR", "fr", "FR"}, {"PL", "pl", "PL"}, {"GB", "", ""}}

// FetchLive returns series covering the last three days and the next two:
// actuals up to now, and forecasts beyond. For Energy-Charts zones the
// forecast is persistence (the value 24 or 48 hours earlier) and the price is
// the day-ahead price where already published; GB comes with the system
// operator's own forecast.
func FetchLive(c *http.Client, zones []ZoneSource, now time.Time) (*Provider, error) {
	from := now.Add(-72 * time.Hour).Truncate(24 * time.Hour)
	to := now.Add(48 * time.Hour).Truncate(24 * time.Hour).Add(24 * time.Hour)
	p := &Provider{Zones: map[string]*Series{}}
	for _, z := range zones {
		var s *Series
		var err error
		if z.Zone == "GB" {
			s, err = FetchGB(c, from, to)
		} else {
			s, err = FetchEnergyCharts(c, z.Zone, z.Country, z.BiddingZone, from, to)
			if err == nil {
				// Beyond now the "actual" is unknown; FetchEnergyCharts has
				// carried the last value forward. The forecast for t is the
				// actual at t-24h, or t-48h when t-24h is itself in the future.
				nowIdx := int(now.Sub(from) / s.Step)
				day := int(24 * time.Hour / s.Step)
				for k := nowIdx; k < len(s.CI); k++ {
					src := k - day
					for src >= nowIdx {
						src -= day
					}
					if src >= 0 {
						s.CIForecast[k] = s.CI[src]
					}
				}
			}
		}
		if err != nil {
			return nil, err
		}
		p.Zones[z.Zone] = s
	}
	return p, nil
}

// CachedLive wraps FetchLive with an on-disk cache: a series younger than ttl
// is read from dir instead of being fetched again. Providers rate-limit, and
// a fleet of agents restarting together must not hammer them.
func CachedLive(c *http.Client, zones []ZoneSource, now time.Time, dir string, ttl time.Duration) (*Provider, error) {
	if dir == "" {
		return FetchLive(c, zones, now)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	p := &Provider{Zones: map[string]*Series{}}
	var missing []ZoneSource
	for _, z := range zones {
		path := filepath.Join(dir, z.Zone+".csv")
		if st, err := os.Stat(path); err == nil && now.Sub(st.ModTime()) < ttl {
			if f, err := os.Open(path); err == nil {
				s, err := ReadCSV(z.Zone, f)
				f.Close()
				if err == nil {
					p.Zones[z.Zone] = s
					continue
				}
			}
		}
		missing = append(missing, z)
	}
	if len(missing) == 0 {
		return p, nil
	}
	fresh, err := FetchLive(c, missing, now)
	if err != nil {
		return nil, err
	}
	for z, s := range fresh.Zones {
		p.Zones[z] = s
		tmp := filepath.Join(dir, z+".csv.tmp")
		if f, err := os.Create(tmp); err == nil {
			s.WriteCSV(f)
			f.Close()
			os.Rename(tmp, filepath.Join(dir, z+".csv"))
		}
	}
	return p, nil
}
