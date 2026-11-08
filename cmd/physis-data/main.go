// Command physis-data downloads the grid signals the experiments use and
// writes one CSV per zone plus SHA256SUMS, so a run can be reproduced from the
// same bytes. The data stays under its providers' terms (see data/README.md);
// it is fetched, not redistributed.
package main

import (
	"crypto/sha256"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/kuberoot-dev/physis/pkg/grid"
)

func main() {
	from := flag.String("from", "2026-07-01", "first day (UTC)")
	to := flag.String("to", "2026-10-01", "day after the last (UTC)")
	out := flag.String("out", "data/grid/2026q3", "output directory")
	flag.Parse()
	f, err := time.Parse("2006-01-02", *from)
	if err != nil {
		log.Fatal(err)
	}
	t, err := time.Parse("2006-01-02", *to)
	if err != nil {
		log.Fatal(err)
	}
	if err := os.MkdirAll(*out, 0o755); err != nil {
		log.Fatal(err)
	}
	c := &http.Client{Timeout: 120 * time.Second}
	zones := []struct{ zone, country, bzn string }{{"DE", "de", "DE-LU"}, {"FR", "fr", "FR"}, {"PL", "pl", "PL"}}
	series := map[string]*grid.Series{}
	for _, z := range zones {
		log.Printf("fetching %s", z.zone)
		s, err := grid.FetchEnergyCharts(c, z.zone, z.country, z.bzn, f, t)
		if err != nil {
			log.Fatal(err)
		}
		series[z.zone] = s
	}
	log.Printf("fetching GB")
	gb, err := grid.FetchGB(c, f, t)
	if err != nil {
		log.Fatal(err)
	}
	series["GB"] = gb
	var names []string
	for z := range series {
		names = append(names, z)
	}
	sort.Strings(names)
	sums, err := os.Create(filepath.Join(*out, "SHA256SUMS"))
	if err != nil {
		log.Fatal(err)
	}
	defer sums.Close()
	for _, z := range names {
		p := filepath.Join(*out, z+".csv")
		w, err := os.Create(p)
		if err != nil {
			log.Fatal(err)
		}
		if err := series[z].WriteCSV(w); err != nil {
			log.Fatal(err)
		}
		w.Close()
		b, _ := os.ReadFile(p)
		fmt.Fprintf(sums, "%x  %s.csv\n", sha256.Sum256(b), z)
	}
	log.Printf("wrote %d zones to %s", len(names), *out)
}
