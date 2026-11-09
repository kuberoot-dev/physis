// Package run executes one (algorithm, effort, physics) on an instance: the
// same code path for the agents on a cluster, the calibration that builds
// quality distributions, and the simulator. The solver runs on the host CPU;
// for emulated physics the device model turns the counted operations into the
// time and energy the real substrate would take. Both are reported.
package run

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"github.com/kuberoot-dev/physis/pkg/device"
	"github.com/kuberoot-dev/physis/pkg/plan"
	"github.com/kuberoot-dev/physis/pkg/problem"
	"github.com/kuberoot-dev/physis/pkg/solve"
)

// Instance is a problem ready to run.
type Instance struct {
	Graph *problem.Graph
	// Reference is the cut quality is measured against.
	Reference float64
	// Classify: model, test batch and the exact accuracy on it.
	Model     *problem.Linear
	Batch     *problem.Dataset
	ExactAcc  float64
	Reservoir *solve.Reservoir
	// HDC holds trained hyperdimensional classifiers by dimension.
	HDC map[int]*solve.HDC
}

// Shape returns what the scheduler sees.
func (in *Instance) Shape() plan.Shape {
	if in.Graph != nil {
		return plan.Shape{Class: plan.MaxCut, N: in.Graph.N, DegreeSum: in.Graph.DegreeSum()}
	}
	return plan.Shape{Class: plan.Classify, N: len(in.Batch.X), DegreeSum: in.Model.MACs()}
}

// Digest of the instance for receipts.
func (in *Instance) Digest() string {
	if in.Graph != nil {
		return in.Graph.Digest()
	}
	return in.Batch.Digest()
}

// Result of one execution.
type Result struct {
	Value        float64 // cut or correct count
	Quality      float64 // value / reference, accuracy / exact accuracy
	Ops          solve.Ops
	ResultDigest string
	// Host: what this process consumed.
	HostCPUSeconds float64
	HostWall       time.Duration
	// Device: what the substrate model charges for the ops.
	Device device.Cost
}

func cpuSeconds() float64 {
	var ru syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &ru); err != nil {
		return 0
	}
	return float64(ru.Utime.Sec+ru.Stime.Sec) + float64(ru.Utime.Usec+ru.Stime.Usec)/1e6
}

// Execute runs the algorithm. For the CPU, device time is the measured CPU
// time of the run and energy is the core's power over it; for emulated
// physics both come from the device model.
func Execute(m *device.Model, algo, effort string, in *Instance, seed uint64) (*Result, error) {
	a, err := plan.Find(algo)
	if err != nil {
		return nil, err
	}
	var e plan.Effort
	for _, x := range a.Efforts {
		if x.Name == effort {
			e = x
		}
	}
	if e.Name == "" {
		return nil, fmt.Errorf("%s has no effort %q", algo, effort)
	}
	if !plan.CanRun(m, a, in.Shape()) {
		return nil, fmt.Errorf("%s cannot run %s on this instance", m.Physics, algo)
	}
	c0, w0 := cpuSeconds(), time.Now()
	var value float64
	var ops solve.Ops
	var spins []int8
	var correct int
	var measured *device.Cost // device time measured on real hardware
	switch algo {
	case "exact":
		r := solve.ExactMaxCut(in.Graph)
		value, ops, spins = r.Cut, r.Ops, r.Spins
	case "local":
		r := solve.LocalSearch(in.Graph, e.Starts, seed)
		value, ops, spins = r.Cut, r.Ops, r.Spins
	case "anneal":
		r := solve.Anneal(in.Graph, e.Sweeps, 2, 0.02, seed)
		value, ops, spins = r.Cut, r.Ops, r.Spins
	case "pbit":
		r := solve.PBitAnneal(in.Graph, e.Sweeps, 0.05, 4, seed)
		value, ops, spins = r.Cut, r.Ops, r.Spins
	case "cim":
		r := solve.CIM(in.Graph, e.Sweeps, 0.05, seed)
		value, ops, spins = r.Cut, r.Ops, r.Spins
	case "qaoa":
		r := solve.QAOA(in.Graph, solve.QAOAParams{Layers: e.Sweeps, Shots: e.Starts, TwoQubitError: 0.005}, seed)
		value, ops, spins = r.Cut, r.Ops, r.Spins
	case "exact-fp":
		r := solve.ClassifyExact(in.Model, in.Batch)
		value, ops, correct = float64(r.Correct), r.Ops, r.Correct
	case "fp8", "mxfp4":
		r := solve.ClassifyLowPrecision(in.Model, in.Batch, algo)
		value, ops, correct = float64(r.Correct), r.Ops, r.Correct
	case "int8":
		r := solve.ClassifyInt8(in.Model, in.Batch)
		value, ops, correct = float64(r.Correct), r.Ops, r.Correct
	case "sc":
		r := solve.ClassifyStochastic(in.Model, in.Batch, e.Sweeps, seed)
		value, ops, correct = float64(r.Correct), r.Ops, r.Correct
	case "hdc":
		h := in.HDC[e.Sweeps]
		if h == nil {
			return nil, fmt.Errorf("no hyperdimensional model of dimension %d", e.Sweeps)
		}
		r := h.Classify(in.Batch)
		value, ops, correct = float64(r.Correct), r.Ops, r.Correct
	case "exact-gpu":
		c, o, d, err := classifyOnGPU(in)
		if err != nil {
			return nil, err
		}
		value, ops, correct, measured = float64(c), o, c, &d
	case "analog":
		r := solve.ClassifyAnalog(in.Model, in.Batch, *m.Analog, seed)
		value, ops, correct = float64(r.Correct), r.Ops, r.Correct
	case "spiking":
		r := solve.ClassifySpiking(in.Model, in.Batch, e.Sweeps, seed)
		value, ops, correct = float64(r.Correct), r.Ops, r.Correct
	case "reservoir":
		if in.Reservoir == nil {
			return nil, fmt.Errorf("reservoir instance without a trained culture")
		}
		res := *in.Reservoir
		res.Steps = e.Sweeps
		r := res.Classify(in.Batch, seed)
		value, ops, correct = float64(r.Correct), r.Ops, r.Correct
	default:
		return nil, fmt.Errorf("no executor for %s", algo)
	}
	res := &Result{Value: value, Ops: ops, HostCPUSeconds: cpuSeconds() - c0, HostWall: time.Since(w0)}
	h := sha256.New()
	if spins != nil {
		for _, s := range spins {
			h.Write([]byte{byte(s)})
		}
		res.Quality = value / in.Reference
	} else {
		fmt.Fprintf(h, "%d", correct)
		acc := float64(correct) / float64(len(in.Batch.X))
		res.Quality = acc / in.ExactAcc
	}
	res.ResultDigest = "sha256:" + hex.EncodeToString(h.Sum(nil))
	if measured != nil {
		res.Device = device.Cost{Seconds: measured.Seconds, Joules: measured.Seconds * m.ActivePowerW.V}
	} else if m.Physics == device.CPU {
		secs := res.HostCPUSeconds
		if secs <= 0 {
			secs = res.HostWall.Seconds()
		}
		res.Device = device.Cost{Seconds: secs, Joules: secs * m.ActivePowerW.V}
	} else {
		res.Device = m.Predict(ops)
	}
	return res, nil
}

// GPUWorker is the command that runs the classifier on a GPU (see
// tools/gpu-worker.py); set from PHYSIS_GPU_WORKER, split on spaces.
var GPUWorker = os.Getenv("PHYSIS_GPU_WORKER")

// classifyOnGPU sends the model and the batch to the GPU worker and returns
// the correct count, the operation count and the measured device time
// (kernel setup plus computation).
func classifyOnGPU(in *Instance) (int, solve.Ops, device.Cost, error) {
	if GPUWorker == "" {
		return 0, solve.Ops{}, device.Cost{}, fmt.Errorf("no GPU worker: set PHYSIS_GPU_WORKER")
	}
	req, err := json.Marshal(map[string]any{"w": in.Model.W, "b": in.Model.B, "x": in.Batch.X})
	if err != nil {
		return 0, solve.Ops{}, device.Cost{}, err
	}
	args := strings.Fields(GPUWorker)
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Stdin = bytes.NewReader(req)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return 0, solve.Ops{}, device.Cost{}, fmt.Errorf("gpu worker: %v: %s", err, stderr.String())
	}
	var resp struct {
		Pred         []int   `json:"pred"`
		Seconds      float64 `json:"seconds"`
		SetupSeconds float64 `json:"setupSeconds"`
		Device       string  `json:"device"`
	}
	if err := json.Unmarshal(out, &resp); err != nil {
		return 0, solve.Ops{}, device.Cost{}, fmt.Errorf("gpu worker: %v", err)
	}
	if len(resp.Pred) != len(in.Batch.Y) || !strings.Contains(resp.Device, "gpu") {
		return 0, solve.Ops{}, device.Cost{}, fmt.Errorf("gpu worker answered %d predictions on %q", len(resp.Pred), resp.Device)
	}
	correct := 0
	for i, p := range resp.Pred {
		if p == in.Batch.Y[i] {
			correct++
		}
	}
	ops := solve.Ops{MACs: int64(len(in.Batch.X)) * int64(in.Model.MACs())}
	return correct, ops, device.Cost{Seconds: resp.SetupSeconds + resp.Seconds}, nil
}
