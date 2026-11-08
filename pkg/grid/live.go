package grid

import (
	"net/http"
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
