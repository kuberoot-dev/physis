// Package device turns operation counts into time and energy for a given
// physics. Every parameter carries its provenance: a measurement, a published
// figure with its reference key (see docs/references.md), or an explicit
// assumption with the range the sensitivity analysis sweeps. Nothing here is
// presented as a measurement of hardware physis does not have.
package device

import (
	"fmt"
	"math"

	"github.com/kuberoot-dev/physis/pkg/solve"
)

// Physics names a computing substrate.
type Physics string

const (
	// CPU: a general-purpose server core. Real: the solver runs on it.
	CPU Physics = "cpu"
	// IsingDigital: a dedicated annealing machine (digital annealer or
	// simulated-bifurcation FPGA). Emulated: the dynamics run on a CPU, the
	// operation count is charged at the machine's published scale.
	IsingDigital Physics = "ising-digital"
	// PBit: a probabilistic-bit computer built from stochastic magnetic tunnel
	// junctions and digital synapses. Emulated.
	PBit Physics = "pbit"
	// AnalogIMC: analog in-memory computing on phase-change memory crossbars.
	// Emulated, including its noise.
	AnalogIMC Physics = "analog-imc"
)

// Value is a model parameter with its provenance.
type Value struct {
	V float64
	// Lo and Hi bound the range swept by the sensitivity analysis.
	Lo, Hi float64
	// Source is "measured", "ref:<key>" or "assumption".
	Source string
	Note   string
}

// Model describes one substrate.
type Model struct {
	Physics  Physics
	Emulated bool
	// OpsPerSecond is the sustained rate of the substrate's elementary
	// operation: spin updates for annealers, MACs for analog crossbars. For a
	// CPU it is calibrated from measured runs.
	OpsPerSecond Value
	// EnergyPerOp in joules, when the substrate is characterised per
	// operation (p-bits, analog crossbars). Zero when ActivePowerW is used.
	EnergyPerOp Value
	// ActivePowerW is the power drawn while computing, used when the
	// substrate is characterised by power and rate (CPU core, FPGA).
	ActivePowerW Value
	// SetupSeconds and SetupJoules are paid once per task: loading a
	// problem, programming conductances.
	SetupSeconds Value
	SetupJoules  Value
	// Calibration: the substrate is unavailable for CalDuration every
	// CalEvery seconds (analog drift compensation, qubit calibration).
	CalEvery    float64
	CalDuration float64
	// MaxSize is the largest problem the substrate holds (spins or weights).
	MaxSize int
	// Precision of the arithmetic in bits; 0 for full double precision.
	PrecisionBits int
	// Analog non-idealities, for AnalogIMC.
	Analog *solve.AnalogParams
}

// Unit is what one "operation" of the substrate is.
func (m *Model) countOps(o solve.Ops) float64 {
	switch m.Physics {
	case AnalogIMC:
		return float64(o.MACs)
	default:
		return float64(o.SpinUpdates)
	}
}

// Cost is a prediction or a measurement of what a run takes.
type Cost struct {
	Seconds float64
	Joules  float64
}

// Predict returns the time and energy the substrate needs for a run of the
// given operation count. For substrates characterised per operation the
// energy is ops x energy-per-op plus setup; for those characterised by power
// it is power x time plus setup.
func (m *Model) Predict(o solve.Ops) Cost {
	n := m.countOps(o)
	t := n/m.OpsPerSecond.V + m.SetupSeconds.V
	var e float64
	if m.EnergyPerOp.V > 0 {
		e = n * m.EnergyPerOp.V
		if m.Physics == PBit {
			// A p-bit update also evaluates its local field in digital
			// synapses: charge the MACs at digital-arithmetic energy.
			e += float64(o.MACs) * EnergyMAC8bit.V
		}
	} else {
		e = (t - m.SetupSeconds.V) * m.ActivePowerW.V
	}
	e += m.SetupJoules.V
	return Cost{Seconds: t, Joules: e}
}

// Validate refuses models that would make the scheduler divide by zero or
// pretend a substrate is free.
func (m *Model) Validate() error {
	if m.OpsPerSecond.V <= 0 {
		return fmt.Errorf("%s: ops per second must be positive", m.Physics)
	}
	if m.EnergyPerOp.V <= 0 && m.ActivePowerW.V <= 0 {
		return fmt.Errorf("%s: either energy per op or active power is required", m.Physics)
	}
	for _, v := range []Value{m.OpsPerSecond, m.EnergyPerOp, m.ActivePowerW, m.SetupSeconds, m.SetupJoules} {
		if v.Source == "" && v.V != 0 {
			return fmt.Errorf("%s: parameter %v has no provenance", m.Physics, v.V)
		}
		if math.IsNaN(v.V) || v.V < 0 {
			return fmt.Errorf("%s: invalid parameter %v", m.Physics, v.V)
		}
	}
	return nil
}

// Published and assumed constants.
var (
	// AMD EPYC 9454P: 48 cores, 290 W default TDP (ref:amd-9454p). Per-core
	// active power is TDP/cores; the socket's uncore makes the true marginal
	// power of one busy core lower when the rest idle, and higher than zero
	// when it does not, hence the wide range.
	CorePowerEPYC9454P = Value{V: 290.0 / 48, Lo: 3, Hi: 9, Source: "ref:amd-9454p", Note: "TDP/cores; marginal power of a busy core"}

	// Energy of an 8-bit multiply plus an 8-bit add at 45 nm (ref:horowitz2014):
	// 0.2 pJ + 0.03 pJ. Newer nodes are cheaper; the range covers 7-45 nm.
	EnergyMAC8bit = Value{V: 0.23e-12, Lo: 0.05e-12, Hi: 0.23e-12, Source: "ref:horowitz2014"}

	// IBM HERMES PCM chip: 9.76 TOPS/W at 63.1 TOPS for 8-bit I/O MVM
	// (ref:legallo2023). One MAC is two operations.
	AnalogEnergyPerMAC = Value{V: 2 / 9.76e12, Lo: 2 / 100e12, Hi: 2 / 1e12, Source: "ref:legallo2023", Note: "measured chip efficiency; range 1-100 TOPS/W covers the AIMC literature"}
	AnalogMACRate      = Value{V: 63.1e12 / 2, Lo: 1e12, Hi: 63.1e12 / 2, Source: "ref:legallo2023"}

	// A stochastic MTJ p-bit dissipates about 2 fJ per random bit
	// (ref:singh2024); flips at GHz scale per p-bit, many in parallel
	// (ref:aadit2022). The aggregate rate is an assumption.
	PBitEnergyPerBit = Value{V: 2e-15, Lo: 1e-15, Hi: 20e-15, Source: "ref:singh2024"}
	PBitAggregateRate = Value{V: 1e11, Lo: 1e9, Hi: 1e12, Source: "assumption", Note: "parallel p-bits on a sparse graph; FPGA emulations reach the lower end"}

	// A simulated-bifurcation FPGA solved 2000-spin all-to-all MaxCut 124x
	// faster than optimised simulated annealing (ref:tatsumura2019). Rate and
	// board power are assumptions in that light.
	IsingRate   = Value{V: 4e9, Lo: 1e8, Hi: 4e10, Source: "assumption", Note: "spin updates per second of a dedicated annealer"}
	IsingPowerW = Value{V: 40, Lo: 15, Hi: 150, Source: "assumption", Note: "board power of an FPGA/ASIC annealer"}
)

// Builtin returns the reference model of each physics. The CPU rate is a
// placeholder until calibrated from measured runs (see learn.Calibrate).
func Builtin(p Physics) *Model {
	switch p {
	case CPU:
		return &Model{Physics: CPU, OpsPerSecond: Value{V: 5e7, Lo: 1e7, Hi: 2e8, Source: "measured", Note: "calibrated per host"},
			ActivePowerW: CorePowerEPYC9454P, MaxSize: 1 << 30}
	case IsingDigital:
		return &Model{Physics: IsingDigital, Emulated: true, OpsPerSecond: IsingRate, ActivePowerW: IsingPowerW,
			SetupSeconds: Value{V: 0.002, Lo: 0.0005, Hi: 0.02, Source: "assumption", Note: "load couplings"},
			SetupJoules:  Value{V: 0.08, Lo: 0.01, Hi: 1, Source: "assumption"},
			MaxSize:      100000, PrecisionBits: 16}
	case PBit:
		return &Model{Physics: PBit, Emulated: true, OpsPerSecond: PBitAggregateRate, EnergyPerOp: PBitEnergyPerBit,
			SetupSeconds: Value{V: 0.001, Lo: 0.0001, Hi: 0.01, Source: "assumption"},
			SetupJoules:  Value{V: 0.01, Lo: 0.001, Hi: 0.1, Source: "assumption"},
			MaxSize:      20000, PrecisionBits: 8}
	case AnalogIMC:
		return &Model{Physics: AnalogIMC, Emulated: true, OpsPerSecond: AnalogMACRate, EnergyPerOp: AnalogEnergyPerMAC,
			SetupSeconds: Value{V: 0.05, Lo: 0.01, Hi: 1, Source: "assumption", Note: "program conductances"},
			SetupJoules:  Value{V: 0.001, Lo: 0.0001, Hi: 0.01, Source: "assumption"},
			CalEvery:     3600, CalDuration: 30,
			MaxSize:      64 * 256 * 256, PrecisionBits: 4,
			Analog:       &solve.AnalogParams{WeightBits: 4, ProgramNoise: 0.03, ReadNoise: 0.02, ADCBits: 8}}
	}
	return nil
}

// AvailableAt returns the first time >= t at which a run of the given length
// fits between calibration windows. Calibration starts at multiples of
// CalEvery and lasts CalDuration.
func (m *Model) AvailableAt(t, length float64) float64 {
	if m.CalEvery <= 0 {
		return t
	}
	for i := 0; i < 1000; i++ {
		k := math.Floor(t / m.CalEvery)
		calEnd := k*m.CalEvery + m.CalDuration
		if t < calEnd {
			t = calEnd
			continue
		}
		next := (k + 1) * m.CalEvery
		if t+length <= next {
			return t
		}
		if length > m.CalEvery-m.CalDuration {
			return t // cannot fit at all; the run spans a calibration
		}
		t = next + m.CalDuration
	}
	return t
}
