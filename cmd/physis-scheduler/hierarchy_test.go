package main

import (
	"regexp"
	"testing"
	"time"
)

// The API server accepts a Lease renewTime with exactly six fractional
// digits; nanoseconds made every renewal fail on a real cluster.
func TestLeaseTimeIsMicroTime(t *testing.T) {
	got := leaseTime(time.Date(2026, 11, 9, 10, 53, 44, 139140070, time.UTC))
	if !regexp.MustCompile(`^\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d\.\d{6}Z$`).MatchString(got) {
		t.Fatalf("renewTime %q is not a MicroTime", got)
	}
}
