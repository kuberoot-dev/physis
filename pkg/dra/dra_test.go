package dra

import "testing"

func slice() map[string]any {
	return map[string]any{"spec": map[string]any{
		"driver": "gpu.example.com",
		"pool":   map[string]any{"name": "Rack 7"},
		"devices": []any{
			map[string]any{"name": "gpu-0", "attributes": map[string]any{
				AttrZone:         map[string]any{"string": "FR"},
				AttrOpsPerSecond: map[string]any{"string": "3e9"},
				"productName":    map[string]any{"string": "Example GPU"},
			}},
			map[string]any{"name": "nic-0", "attributes": map[string]any{}},
		},
	}}
}

func TestDevicesBecomeSubstrates(t *testing.T) {
	ds := Devices(slice())
	if len(ds) != 2 || ds[0].Attributes[AttrZone] != "FR" {
		t.Fatalf("devices: %+v", ds)
	}
	spec, ok, err := ToSubstrate(ds[0], Defaults{Zone: "DE", Operator: "op", Jurisdiction: "EU"})
	if err != nil || !ok {
		t.Fatalf("gpu device not mapped: %v", err)
	}
	if spec.Physics != "gpu" || spec.Zone != "FR" || spec.Jurisdiction != "EU" || spec.Emulated {
		t.Fatalf("spec: %+v", spec)
	}
	src := map[string]string{}
	for _, p := range spec.Model {
		src[p.Name] = p.Source
	}
	// What the driver states is cited to it; the rest is marked as an
	// assumption, never passed off as known.
	if src["opsPerSecond"] != "ref:dra:gpu.example.com" || src["activePowerW"] != "assumption" {
		t.Fatalf("provenance: %v", src)
	}
	if n := SubstrateName(ds[0]); n != "dra-rack-7-gpu-0" {
		t.Fatalf("name %q", n)
	}
	// A device of unknown physics (a NIC from a network driver) is skipped,
	// not guessed.
	nic := ds[1]
	nic.Driver = "nic.example.com"
	if _, ok, _ := ToSubstrate(nic, Defaults{}); ok {
		t.Fatal("a NIC became a substrate")
	}
}

func TestBadNumberIsRefused(t *testing.T) {
	d := Device{Driver: "x", Pool: "p", Name: "d", Attributes: map[string]any{AttrPhysics: "gpu", AttrActivePowerW: "lots"}}
	if _, _, err := ToSubstrate(d, Defaults{}); err == nil {
		t.Fatal("a non-numeric parameter was accepted")
	}
}
