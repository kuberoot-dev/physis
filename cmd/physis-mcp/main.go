// Command physis-mcp puts the physis API in front of AI agents as a Model
// Context Protocol server over stdio (P14: the agent is the first user of the
// API). An agent's host starts it with a kubeconfig whose identity is the
// agent's; it then lists substrates, asks for plans without running them,
// submits idempotent problems, reads their status and verifies receipts
// offline. It adds no authority of its own: every call is an ordinary API
// request under that identity, and the scheduler applies the same mandates,
// budgets and rate limits as to any other caller.
//
//	physis-mcp -kubeconfig ~/.kube/agent -namespace agents
package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/kuberoot-dev/physis/pkg/kube"
)

var (
	kubeconfig  = flag.String("kubeconfig", "", "kubeconfig (empty: in-cluster)")
	namespace   = flag.String("namespace", "default", "namespace for problems when a call names none")
	staleAfter  = flag.Duration("stale-after", 30*time.Second, "a substrate silent this long is reported as not alive")
	planTimeout = flag.Duration("plan-timeout", 30*time.Second, "how long the plan tool waits for the scheduler by default")
)

// version is set at build time.
var version = "dev"

func main() {
	flag.Parse()
	// stdout carries the protocol; everything else goes to stderr.
	log.SetOutput(os.Stderr)
	c, err := kube.New(*kubeconfig)
	if err != nil {
		log.Fatal(err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	t := &tools{c: c, namespace: *namespace, staleAfter: *staleAfter, planTimeout: *planTimeout, poll: 500 * time.Millisecond, now: time.Now}
	if err := t.server().Serve(ctx, os.Stdin, os.Stdout); err != nil && ctx.Err() == nil {
		log.Fatal(err)
	}
}
