// Package devicelink is the link between a physis agent and a device core:
// one JSON object per line over a byte stream. The device core computes and
// signs, and holds the signing key; the agent around it does everything that
// needs a network (claiming, heartbeats, delivering reports). The stream can
// be the pipes of a WASI module in a sandbox, a serial port to a
// microcontroller, or anything else that carries lines. Nothing in it
// assumes Linux, sockets or a filesystem on the device side.
package devicelink

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"

	v1 "github.com/kuberoot-dev/physis/api/v1alpha1"
	"github.com/kuberoot-dev/physis/pkg/device"
	"github.com/kuberoot-dev/physis/pkg/receipt"
	"github.com/kuberoot-dev/physis/pkg/solve"
)

// Request is one call to the device.
type Request struct {
	Op string `json:"op"` // hello, run or sign
	// run
	Instance v1.InstanceRef `json:"instance,omitempty"`
	Algo     string         `json:"algo,omitempty"`
	Effort   string         `json:"effort,omitempty"`
	Seed     uint64         `json:"seed,omitempty"`
	// sign
	Receipt *receipt.Receipt `json:"receipt,omitempty"`
}

// Response is the device's answer.
type Response struct {
	Error string `json:"error,omitempty"`
	// hello
	PublicKey string `json:"publicKey,omitempty"`
	Seq       uint64 `json:"seq,omitempty"`
	Head      string `json:"head,omitempty"`
	Physics   string `json:"physics,omitempty"`
	// run
	Value          float64     `json:"value,omitempty"`
	Quality        float64     `json:"quality,omitempty"`
	ResultDigest   string      `json:"resultDigest,omitempty"`
	InstanceDigest string      `json:"instanceDigest,omitempty"`
	Ops            solve.Ops   `json:"ops,omitempty"`
	Device         device.Cost `json:"device,omitempty"`
	HostCPUSeconds float64     `json:"hostCpuSeconds,omitempty"`
	// sign
	Receipt *receipt.Receipt `json:"receipt,omitempty"`
}

// Link talks to one device core.
type Link struct {
	mu  sync.Mutex
	in  io.Writer
	out *bufio.Reader
	cmd *exec.Cmd
	// Chain state as the device last reported it.
	pub  string
	seq  uint64
	head string
}

// Start runs the device command (split on spaces) and says hello.
func Start(command string, stderr io.Writer) (*Link, error) {
	args := strings.Fields(command)
	if len(args) == 0 {
		return nil, fmt.Errorf("empty device command")
	}
	cmd := exec.Command(args[0], args[1:]...)
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return Open(in, out, cmd)
}

// Open says hello over an existing stream.
func Open(in io.Writer, out io.Reader, cmd *exec.Cmd) (*Link, error) {
	l := &Link{in: in, out: bufio.NewReaderSize(out, 1<<20), cmd: cmd}
	r, err := l.call(Request{Op: "hello"})
	if err != nil {
		return nil, err
	}
	l.pub, l.seq, l.head = r.PublicKey, r.Seq, r.Head
	return l, nil
}

func (l *Link) call(q Request) (*Response, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	b, err := json.Marshal(q)
	if err != nil {
		return nil, err
	}
	if _, err := l.in.Write(append(b, '\n')); err != nil {
		return nil, fmt.Errorf("device link: %w", err)
	}
	line, err := l.out.ReadBytes('\n')
	if err != nil {
		return nil, fmt.Errorf("device link: %w", err)
	}
	var r Response
	if err := json.Unmarshal(line, &r); err != nil {
		return nil, fmt.Errorf("device link: %w", err)
	}
	if r.Error != "" {
		return nil, fmt.Errorf("device: %s", r.Error)
	}
	return &r, nil
}

// Run asks the device to compute.
func (l *Link) Run(in v1.InstanceRef, algo, effort string, seed uint64) (*Response, error) {
	return l.call(Request{Op: "run", Instance: in, Algo: algo, Effort: effort, Seed: seed})
}

// Sign has the device sign a receipt with the key only it holds and chain
// it to its previous ones; it implements the agent's signer.
func (l *Link) Sign(r *receipt.Receipt) {
	resp, err := l.call(Request{Op: "sign", Receipt: r})
	if err != nil || resp.Receipt == nil {
		r.Signature = "" // an unsigned receipt fails verification, as it should
		return
	}
	*r = *resp.Receipt
	l.mu.Lock()
	l.seq, l.head = r.Seq, r.Hash()
	l.mu.Unlock()
}

// PublicKey is the device's key, hex.
func (l *Link) PublicKey() string { return l.pub }

// Head is the device's chain position.
func (l *Link) Head() (uint64, string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.seq, l.head
}
