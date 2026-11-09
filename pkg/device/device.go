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
	// QPU: a superconducting gate-model quantum processor running QAOA.
	// Emulated by a state-vector simulator with a depolarising error model.
	QPU Physics = "qpu"
	// OpticalIsing: a measurement-feedback coherent Ising machine (optical
	// parametric oscillator pulses in a fibre loop). Emulated.
	OpticalIsing Physics = "optical-ising"
	// SFQ: single-flux-quantum superconducting digital logic at 4 K running
	// the annealing dynamics. Emulated; the cryocooler is charged.
	SFQ Physics = "sfq"
	// Adiabatic: reversible (adiabatic) CMOS that recovers part of its
	// switching energy at the price of a slower clock. Emulated.
	Adiabatic Physics = "adiabatic"
	// Neuromorphic: a spiking chip charging per synaptic event. Emulated.
	Neuromorphic Physics = "neuromorphic"
	// Photonic: optical matrix-vector multiplication with lasers and
	// detectors. Emulated, with its noise.
	Photonic Physics = "photonic"
	// Organoid: a living neuronal culture used as a reservoir, with a
	// readout on a conventional computer. Emulated by an echo-state network;
	// the life-support system is charged.
	Organoid Physics = "organoid"
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
	case CPU:
		// A core pays for both the local-field arithmetic and the update
		// decision; its rate is calibrated on this sum.
		return float64(o.SpinUpdates + o.MACs)
	case AnalogIMC, Photonic, Neuromorphic:
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
	// Dynamic energy per operation plus static power over the run time: an
	// SFQ chip pays per switch and for its cryocooler, a photonic core per
	// MAC and for its lasers, a CPU core only by power.
	e := n*m.EnergyPerOp.V + (t-m.SetupSeconds.V)*m.ActivePowerW.V
	if m.Physics == PBit {
		// A p-bit update also evaluates its local field in digital
		// synapses: charge the MACs at digital-arithmetic energy.
		e += float64(o.MACs) * EnergyMAC8bit.V
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
	PBitEnergyPerBit  = Value{V: 2e-15, Lo: 1e-15, Hi: 20e-15, Source: "ref:singh2024"}
	PBitAggregateRate = Value{V: 1e11, Lo: 1e9, Hi: 1e12, Source: "assumption", Note: "parallel p-bits on a sparse graph; FPGA emulations reach the lower end"}

	// A simulated-bifurcation FPGA solved 2000-spin all-to-all MaxCut 124x
	// faster than optimised simulated annealing (ref:tatsumura2019). Rate and
	// board power are assumptions in that light.
	IsingRate   = Value{V: 4e9, Lo: 1e8, Hi: 4e10, Source: "assumption", Note: "spin updates per second of a dedicated annealer"}
	IsingPowerW = Value{V: 40, Lo: 15, Hi: 150, Source: "assumption", Note: "board power of an FPGA/ASIC annealer"}
)

// Further published and assumed constants for the extended physics set.
var (
	// A superconducting quantum computer with its dilution refrigerator,
	// chilled water and electronics draws about 26 kW in the worst case
	// (ref:arute2019supp).
	QPUPowerW = Value{V: 26000, Lo: 15000, Hi: 30000, Source: "ref:arute2019supp"}
	// Time per shot: circuit, readout and reset of a superconducting device.
	QPUShotSeconds = Value{V: 2e-4, Lo: 5e-5, Hi: 1e-3, Source: "assumption"}
	// Cryogenic refrigeration at 4 K: the Carnot limit is about 74 W of wall
	// power per watt of heat removed, the best large plants reach below
	// 400 W/W (ref:primavera2021), small coolers need 5-7.5 kW/W (ref:cryo4k).
	// 740 W/W is our assumption for a mid-sized plant; it errs against SFQ.
	Cryo4KWallPerWatt = Value{V: 740, Lo: 740, Hi: 7500, Source: "assumption"}
	// Loihi: about 20-25 pJ per synaptic operation (ref:loihi).
	NeuroEnergyPerSynop = Value{V: 23.6e-12, Lo: 4e-12, Hi: 25e-12, Source: "ref:loihi"}
	// A photonic matrix processor measured 0.27 pJ/MAC of laser energy at
	// 10 mW and 36.7 GBaud (ref:photonic2024); converters add more.
	PhotonicEnergyPerMAC = Value{V: 0.27e-12, Lo: 0.02e-12, Hi: 2e-12, Source: "ref:photonic2024"}
	// A Cortical Labs CL1 is reported at about 20 W per device
	// (ref:cl1power); 30 W leaves room for life support and electronics.
	OrganoidPowerW = Value{V: 30, Lo: 20, Hi: 100, Source: "ref:cl1power"}
	// Vaire's adiabatic test chip used about 30% less energy than a
	// conventional one for the same task (ref:vaire).
	AdiabaticSaving = Value{V: 0.30, Lo: 0.1, Hi: 0.9, Source: "ref:vaire", Note: "measured 30%; the range reaches the long-term claims"}
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
			MaxSize: 64 * 256 * 256, PrecisionBits: 4,
			Analog: &solve.AnalogParams{WeightBits: 4, ProgramNoise: 0.03, ReadNoise: 0.02, ADCBits: 8}}
	case QPU:
		return &Model{Physics: QPU, Emulated: true,
			OpsPerSecond: Value{V: 1 / QPUShotSeconds.V, Lo: 1 / QPUShotSeconds.Hi, Hi: 1 / QPUShotSeconds.Lo, Source: "assumption", Note: "shots per second"},
			ActivePowerW: QPUPowerW,
			SetupSeconds: Value{V: 2, Lo: 0.5, Hi: 30, Source: "assumption", Note: "transpile and load the circuit"},
			SetupJoules:  Value{V: 2 * QPUPowerW.V, Lo: 0.5 * QPUPowerW.Lo, Hi: 30 * QPUPowerW.Hi, Source: "assumption", Note: "the machine draws power while loading"},
			CalEvery:     6 * 3600, CalDuration: 900,
			MaxSize: 20, PrecisionBits: 0}
	case OpticalIsing:
		return &Model{Physics: OpticalIsing, Emulated: true,
			OpsPerSecond: Value{V: 1e9, Lo: 1e8, Hi: 1e10, Source: "assumption", Note: "pulse rate of a time-multiplexed OPO loop"},
			ActivePowerW: Value{V: 1000, Lo: 200, Hi: 3000, Source: "assumption", Note: "pump lasers, amplifiers, feedback FPGA"},
			SetupSeconds: Value{V: 0.01, Lo: 0.001, Hi: 0.1, Source: "assumption"},
			MaxSize:      100000, PrecisionBits: 8}
	case SFQ:
		return &Model{Physics: SFQ, Emulated: true,
			OpsPerSecond: Value{V: 2e10, Lo: 1e9, Hi: 1e11, Source: "assumption", Note: "spin updates per second at tens of GHz clock"},
			// About 1e5 junction switches of ~1e-19 J per spin update,
			// lifted to the wall by the cryocooler.
			EnergyPerOp: Value{V: 1e-14 * Cryo4KWallPerWatt.V, Lo: 1e-15 * Cryo4KWallPerWatt.Lo, Hi: 1e-13 * Cryo4KWallPerWatt.Hi, Source: "assumption", Note: "1e5 switches x 1e-19 J x cryo overhead"},
			// The cryostat's own heat load of about 1 W at 4 K, at the wall.
			ActivePowerW: Value{V: 1 * Cryo4KWallPerWatt.V, Lo: 0.1 * Cryo4KWallPerWatt.Lo, Hi: 5 * Cryo4KWallPerWatt.Hi, Source: "assumption", Note: "static heat load x cryo overhead"},
			SetupSeconds: Value{V: 0.001, Lo: 0.0001, Hi: 0.01, Source: "assumption"},
			MaxSize:      50000, PrecisionBits: 16}
	case Adiabatic:
		base := IsingPowerW.V / IsingRate.V
		return &Model{Physics: Adiabatic, Emulated: true,
			OpsPerSecond: Value{V: IsingRate.V / 2, Lo: IsingRate.Lo / 2, Hi: IsingRate.Hi / 2, Source: "assumption", Note: "half the clock of a conventional annealer"},
			EnergyPerOp:  Value{V: base * (1 - AdiabaticSaving.V), Lo: base * (1 - AdiabaticSaving.Hi), Hi: base * (1 - AdiabaticSaving.Lo), Source: "assumption", Note: "assumed conventional annealer energy (40 W at 4e9 ops/s) minus the 30% saving measured by Vaire (ref:vaire)"},
			SetupSeconds: Value{V: 0.002, Lo: 0.0005, Hi: 0.02, Source: "assumption"},
			MaxSize:      100000, PrecisionBits: 16}
	case Neuromorphic:
		return &Model{Physics: Neuromorphic, Emulated: true,
			OpsPerSecond: Value{V: 1e10, Lo: 1e9, Hi: 1e11, Source: "assumption", Note: "synaptic events per second of one chip"},
			EnergyPerOp:  NeuroEnergyPerSynop,
			ActivePowerW: Value{V: 1, Lo: 0.1, Hi: 5, Source: "assumption", Note: "static power"},
			SetupSeconds: Value{V: 0.01, Lo: 0.001, Hi: 0.1, Source: "assumption", Note: "load synaptic weights"},
			MaxSize:      1 << 20, PrecisionBits: 8}
	case Photonic:
		return &Model{Physics: Photonic, Emulated: true,
			OpsPerSecond: Value{V: 1e12, Lo: 1e11, Hi: 1e13, Source: "assumption", Note: "MACs per second"},
			EnergyPerOp:  PhotonicEnergyPerMAC,
			ActivePowerW: Value{V: 5, Lo: 1, Hi: 20, Source: "assumption", Note: "lasers, modulator drivers and converters while on"},
			SetupSeconds: Value{V: 0.001, Lo: 0.0001, Hi: 0.01, Source: "assumption", Note: "set the modulators"},
			MaxSize:      1 << 16, PrecisionBits: 6,
			Analog: &solve.AnalogParams{WeightBits: 6, ProgramNoise: 0.01, ReadNoise: 0.01, ADCBits: 8}}
	case Organoid:
		// 300 units stimulated in 10 ms steps: 3e4 unit-steps per second.
		return &Model{Physics: Organoid, Emulated: true,
			OpsPerSecond: Value{V: 3e4, Lo: 3e3, Hi: 3e5, Source: "assumption", Note: "unit-steps per second; 10 ms per stimulation step"},
			ActivePowerW: OrganoidPowerW,
			SetupSeconds: Value{V: 1, Lo: 0.1, Hi: 10, Source: "assumption"},
			// Daily medium change: the culture is unavailable for an hour.
			CalEvery: 86400, CalDuration: 3600,
			MaxSize: 1 << 20}
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

// All lists every physics physis knows.
var All = []Physics{CPU, IsingDigital, PBit, AnalogIMC, QPU, OpticalIsing, SFQ, Adiabatic, Neuromorphic, Photonic, Organoid}
