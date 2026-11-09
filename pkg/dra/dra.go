// Package dra bridges Kubernetes Dynamic Resource Allocation to physis: every
// device a DRA driver publishes in a ResourceSlice becomes a Substrate. DRA
// already describes devices with structured attributes; what it lacks is a
// cost model. A driver that knows its device can state one with attributes in
// the physis.kuberoot.dev domain; whatever it does not state falls back to
// the reference model of the physics and is marked as an assumption, so the
// gap stays visible (principle P3).
package dra

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	v1 "github.com/kuberoot-dev/physis/api/v1alpha1"
	"github.com/kuberoot-dev/physis/pkg/device"
)

// Attribute names a driver may set on a device.
const (
	Prefix           = "physis.kuberoot.dev/"
	AttrPhysics      = Prefix + "physics"
	AttrZone         = Prefix + "zone"
	AttrOperator     = Prefix + "operator"
	AttrJurisdiction = Prefix + "jurisdiction"
	AttrEmulated     = Prefix + "emulated"
	// Model parameters, as strings holding numbers (DRA attributes have no
	// floating-point type).
	AttrOpsPerSecond = Prefix + "opsPerSecond"
	AttrEnergyPerOpJ = Prefix + "energyPerOpJ"
	AttrActivePowerW = Prefix + "activePowerW"
	AttrSetupSeconds = Prefix + "setupSeconds"
	AttrSetupJ       = Prefix + "setupJ"
	// SourceAnnotation records where a bridged substrate came from.
	SourceAnnotation = Prefix + "source"
)

// Device is one device of a ResourceSlice, as the bridge reads it.
type Device struct {
	Driver, Pool, Name string
	// Attributes hold the device's attribute values, already unwrapped from
	// DRA's typed form (string, int, bool or version).
	Attributes map[string]any
}

// Defaults fill what a device does not state.
type Defaults struct {
	Zone, Operator, Jurisdiction string
}

// driverPhysics guesses the physics from well-known driver names when the
// device does not say.
func driverPhysics(driver string) device.Physics {
	switch {
	case strings.Contains(driver, "gpu"):
		return device.GPU
	}
	return ""
}

var unsafeName = regexp.MustCompile(`[^a-z0-9-]+`)

// SubstrateName is the bridged substrate's name: dra-<pool>-<device>.
func SubstrateName(d Device) string {
	n := strings.ToLower("dra-" + d.Pool + "-" + d.Name)
	n = strings.Trim(unsafeName.ReplaceAllString(n, "-"), "-")
	if len(n) > 63 {
		n = strings.TrimRight(n[:63], "-")
	}
	return n
}

// ToSubstrate maps a device to a substrate spec. It reports false for a
// device whose physics is neither stated nor recognisable.
func ToSubstrate(d Device, def Defaults) (v1.SubstrateSpec, bool, error) {
	str := func(k, fallback string) string {
		if v, ok := d.Attributes[k].(string); ok && v != "" {
			return v
		}
		return fallback
	}
	phys := device.Physics(str(AttrPhysics, string(driverPhysics(d.Driver))))
	if phys == "" {
		return v1.SubstrateSpec{}, false, nil
	}
	ref := device.Builtin(phys)
	if ref == nil {
		return v1.SubstrateSpec{}, false, fmt.Errorf("device %s/%s/%s: unknown physics %q", d.Driver, d.Pool, d.Name, phys)
	}
	emulated := false
	if v, ok := d.Attributes[AttrEmulated].(bool); ok {
		emulated = v
	}
	spec := v1.SubstrateSpec{Physics: string(phys), Zone: str(AttrZone, def.Zone), Operator: str(AttrOperator, def.Operator),
		Jurisdiction: str(AttrJurisdiction, def.Jurisdiction), Emulated: emulated, Slots: 1}
	stated := fmt.Sprintf("ref:dra:%s", d.Driver)
	for _, p := range []struct {
		name, attr string
		ref        device.Value
	}{
		{"opsPerSecond", AttrOpsPerSecond, ref.OpsPerSecond},
		{"energyPerOpJ", AttrEnergyPerOpJ, ref.EnergyPerOp},
		{"activePowerW", AttrActivePowerW, ref.ActivePowerW},
		{"setupSeconds", AttrSetupSeconds, ref.SetupSeconds},
		{"setupJ", AttrSetupJ, ref.SetupJoules},
	} {
		if s, ok := d.Attributes[p.attr].(string); ok {
			v, err := strconv.ParseFloat(s, 64)
			if err != nil || v < 0 {
				return v1.SubstrateSpec{}, false, fmt.Errorf("device %s: attribute %s=%q is not a number", d.Name, p.attr, s)
			}
			spec.Model = append(spec.Model, v1.Parameter{Name: p.name, Value: v, Source: stated,
				Note: fmt.Sprintf("declared by the DRA driver in pool %s", d.Pool)})
			continue
		}
		if p.ref.V == 0 {
			continue
		}
		spec.Model = append(spec.Model, v1.Parameter{Name: p.name, Value: p.ref.V, Lo: p.ref.Lo, Hi: p.ref.Hi, Source: "assumption",
			Note: "not described by the DRA driver; the reference value for this physics"})
	}
	return spec, true, nil
}

// Devices unwraps the devices of a ResourceSlice object (resource.k8s.io/v1,
// as unstructured content).
func Devices(slice map[string]any) []Device {
	spec, _ := slice["spec"].(map[string]any)
	driver, _ := spec["driver"].(string)
	pool := ""
	if p, ok := spec["pool"].(map[string]any); ok {
		pool, _ = p["name"].(string)
	}
	var out []Device
	devs, _ := spec["devices"].([]any)
	for _, x := range devs {
		dm, _ := x.(map[string]any)
		name, _ := dm["name"].(string)
		d := Device{Driver: driver, Pool: pool, Name: name, Attributes: map[string]any{}}
		attrs, _ := dm["attributes"].(map[string]any)
		for k, v := range attrs {
			typed, _ := v.(map[string]any)
			for _, t := range []string{"string", "int", "bool", "version"} {
				if val, ok := typed[t]; ok {
					d.Attributes[k] = val
				}
			}
		}
		out = append(out, d)
	}
	return out
}
