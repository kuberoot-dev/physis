// Package physis holds the kubepkg recipe and Helm chart of physis. The test
// keeps the chart's copy of the calibration in step with results/, which is
// what the stands and the article use: a stale copy left new physics
// uncalibrated on a cluster, and nothing else caught it.
package physis

import (
	"bytes"
	"os"
	"testing"
)

func TestChartCalibrationMatchesResults(t *testing.T) {
	for _, f := range []string{"learn.json", "best-found.json"} {
		chart, err := os.ReadFile("charts/physis/files/" + f)
		if err != nil {
			t.Fatal(err)
		}
		res, err := os.ReadFile("../../../results/" + f)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(chart, res) {
			t.Errorf("charts/physis/files/%s differs from results/%s: copy it", f, f)
		}
	}
}
