// Command physis-device is the core of a substrate's agent without the
// agent: it computes and signs, and keeps the signing key and the receipt
// chain in its state directory. It speaks the devicelink protocol (one JSON
// object per line) on stdin and stdout and needs no network, no sockets and
// no Linux. It builds for WASI (GOOS=wasip1 GOARCH=wasm) and runs in a
// WebAssembly sandbox; a physis-agent started with -device talks to it and
// does the networking around it. The same protocol fits a microcontroller on
// a serial line.
package main

import (
	"bufio"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/kuberoot-dev/physis/pkg/device"
	"github.com/kuberoot-dev/physis/pkg/devicelink"
	"github.com/kuberoot-dev/physis/pkg/instances"
	"github.com/kuberoot-dev/physis/pkg/receipt"
	"github.com/kuberoot-dev/physis/pkg/run"
)

func main() {
	physics := flag.String("physics", "cpu", "physics this device core stands for")
	state := flag.String("state", "/state", "directory with the key and the chain")
	data := flag.String("data", "/data", "directory with gset/, digits/ and best-found.json")
	flag.Parse()
	m := device.Builtin(device.Physics(*physics))
	if m == nil {
		fmt.Fprintf(os.Stderr, "unknown physics %q\n", *physics)
		os.Exit(2)
	}
	priv, err := loadKey(*state)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	seq, head := loadChain(*state)
	signer := receipt.NewSigner(priv, seq, head)
	best := map[string]float64{}
	if b, err := os.ReadFile(filepath.Join(*data, "best-found.json")); err == nil {
		_ = json.Unmarshal(b, &best)
	}
	loader := &instances.Loader{Dir: *data}
	in := bufio.NewReaderSize(os.Stdin, 1<<20)
	out := bufio.NewWriter(os.Stdout)
	reply := func(r devicelink.Response) {
		b, _ := json.Marshal(r)
		out.Write(append(b, '\n'))
		out.Flush()
	}
	for {
		line, err := in.ReadBytes('\n')
		if err != nil {
			return
		}
		var q devicelink.Request
		if err := json.Unmarshal(line, &q); err != nil {
			reply(devicelink.Response{Error: err.Error()})
			continue
		}
		switch q.Op {
		case "hello":
			s, h := signer.Head()
			reply(devicelink.Response{PublicKey: signer.PublicKey(), Seq: s, Head: h, Physics: *physics})
		case "run":
			inst, err := loader.Load(q.Instance, best)
			if err != nil {
				reply(devicelink.Response{Error: err.Error()})
				continue
			}
			r, err := run.Execute(m, q.Algo, q.Effort, inst, q.Seed)
			if err != nil {
				reply(devicelink.Response{Error: err.Error()})
				continue
			}
			reply(devicelink.Response{Value: r.Value, Quality: r.Quality, ResultDigest: r.ResultDigest, InstanceDigest: inst.Digest(),
				Ops: r.Ops, Device: r.Device, HostCPUSeconds: r.HostCPUSeconds})
		case "sign":
			if q.Receipt == nil {
				reply(devicelink.Response{Error: "no receipt"})
				continue
			}
			r := *q.Receipt
			signer.Sign(&r)
			s, h := signer.Head()
			if err := saveChain(*state, s, h); err != nil {
				reply(devicelink.Response{Error: err.Error()})
				continue
			}
			reply(devicelink.Response{Receipt: &r})
		default:
			reply(devicelink.Response{Error: "unknown op " + q.Op})
		}
	}
}

func loadKey(dir string) (ed25519.PrivateKey, error) {
	p := filepath.Join(dir, "device.key")
	if b, err := os.ReadFile(p); err == nil && len(b) == ed25519.PrivateKeySize {
		return ed25519.PrivateKey(b), nil
	}
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(p, priv, 0o600); err != nil {
		return nil, fmt.Errorf("device key: %w", err)
	}
	return priv, nil
}

type chain struct {
	Seq  uint64 `json:"seq"`
	Head string `json:"head"`
}

func loadChain(dir string) (uint64, string) {
	var c chain
	if b, err := os.ReadFile(filepath.Join(dir, "chain.json")); err == nil {
		_ = json.Unmarshal(b, &c)
	}
	return c.Seq, c.Head
}

func saveChain(dir string, seq uint64, head string) error {
	b, _ := json.Marshal(chain{seq, head})
	tmp := filepath.Join(dir, "chain.json.tmp")
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(dir, "chain.json"))
}
