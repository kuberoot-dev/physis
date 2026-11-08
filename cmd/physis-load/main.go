// Command physis-load submits a stream of problems to a live physis
// installation in real time, waits for them, and writes one CSV row per
// problem: what was asked, what was planned, what happened, and the receipt.
package main

import (
	"context"
	"encoding/csv"
	"flag"
	"fmt"
	"log"
	"math/rand/v2"
	"os"
	"strconv"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	v1 "github.com/kuberoot-dev/physis/api/v1alpha1"
	"github.com/kuberoot-dev/physis/pkg/kube"
	"github.com/kuberoot-dev/physis/pkg/receipt"
)

func main() {
	kc := flag.String("kubeconfig", "", "kubeconfig")
	ns := flag.String("namespace", "load", "namespace")
	n := flag.Int("n", 120, "problems")
	every := flag.Duration("every", 5*time.Second, "mean interval between submissions")
	out := flag.String("out", "results/live.csv", "output CSV")
	flag.Parse()
	c, err := kube.New(*kc)
	if err != nil {
		log.Fatal(err)
	}
	ctx := context.Background()
	c.D.Resource(schema.GroupVersionResource{Version: "v1", Resource: "namespaces"}).Create(ctx, &unstructured.Unstructured{Object: map[string]any{"apiVersion": "v1", "kind": "Namespace", "metadata": map[string]any{"name": *ns}}}, metav1.CreateOptions{})
	rng := rand.New(rand.NewPCG(42, 7))
	gs := []string{"G1", "G2", "G11", "G14", "G43", "G22"}
	pols := []string{"min-energy", "min-carbon", "fastest", "min-cost"}
	var names []string
	for i := 0; i < *n; i++ {
		var spec v1.ProblemSpec
		switch x := rng.Float64(); {
		case x < 0.45:
			spec = v1.ProblemSpec{Class: "maxcut", Instance: v1.InstanceRef{Gset: gs[rng.IntN(len(gs))]}, Quality: []float64{0.98, 0.99, 0.995}[rng.IntN(3)]}
		case x < 0.6:
			spec = v1.ProblemSpec{Class: "maxcut", Instance: v1.InstanceRef{Random: &v1.RandomGraph{N: 12 + 4*rng.IntN(3), P: 0.4, Seed: uint64(1000*(12+4*rng.IntN(3)) + rng.IntN(4))}}, Quality: 0.99}
		default:
			size := []int{100, 500, 1797}[rng.IntN(3)]
			spec = v1.ProblemSpec{Class: "classify", Instance: v1.InstanceRef{Digits: &v1.DigitsBatch{Size: size, Offset: 0}}, Quality: []float64{0.97, 0.99}[rng.IntN(2)]}
		}
		spec.Confidence = 0.9
		spec.Policy = pols[rng.IntN(len(pols))]
		spec.DeadlineSeconds = 120
		if rng.Float64() < 0.3 {
			spec.Zones = []string{"DE", "FR", "PL"}
		}
		name := fmt.Sprintf("p%04d", i)
		p := &kube.Problem{Meta: metav1.ObjectMeta{Name: name, Namespace: *ns}, Spec: spec}
		u, _ := kube.Encode("Problem", p)
		if _, err := c.D.Resource(kube.Problems).Namespace(*ns).Create(ctx, u, metav1.CreateOptions{}); err != nil {
			log.Printf("%s: %v", name, err)
			continue
		}
		names = append(names, name)
		time.Sleep(time.Duration(rng.ExpFloat64() * float64(*every)))
	}
	log.Printf("submitted %d; waiting", len(names))
	deadline := time.Now().Add(20 * time.Minute)
	for time.Now().Before(deadline) {
		done := 0
		for _, nm := range names {
			u, err := c.D.Resource(kube.Problems).Namespace(*ns).Get(ctx, nm, metav1.GetOptions{})
			if err != nil {
				continue
			}
			ph, _, _ := unstructured.NestedString(u.Object, "status", "phase")
			if ph == v1.PhaseSucceeded || ph == v1.PhaseRejected || ph == v1.PhaseFailed {
				done++
			}
		}
		if done == len(names) {
			break
		}
		time.Sleep(5 * time.Second)
	}
	file, err := os.Create(*out)
	if err != nil {
		log.Fatal(err)
	}
	w := csv.NewWriter(file)
	w.Write([]string{"problem", "class", "instance", "policy", "quality_target", "phase", "substrate", "physics", "zone", "algorithm", "effort",
		"pred_s", "pred_j", "pred_gco2", "p_quality", "quality", "device_s", "device_j", "host_cpu_s", "gco2", "ci", "wait_s", "receipt_seq", "message"})
	for _, nm := range names {
		u, err := c.D.Resource(kube.Problems).Namespace(*ns).Get(ctx, nm, metav1.GetOptions{})
		if err != nil {
			continue
		}
		p, _ := kube.Decode[v1.ProblemSpec, v1.ProblemStatus](u)
		inst := p.Spec.Instance.Gset
		if p.Spec.Instance.Random != nil {
			inst = fmt.Sprintf("er%d", p.Spec.Instance.Random.N)
		}
		if p.Spec.Instance.Digits != nil {
			inst = fmt.Sprintf("digits%d", p.Spec.Instance.Digits.Size)
		}
		row := []string{nm, p.Spec.Class, inst, p.Spec.Policy, f(p.Spec.Quality), p.Status.Phase}
		if pl := p.Status.Plan; pl != nil {
			row = append(row, pl.Substrate, string(pl.Physics), pl.Zone, pl.Algorithm, pl.Effort, f(pl.Seconds), f(pl.Joules), f(pl.GCO2), f(pl.PQuality))
		} else {
			row = append(row, "", "", "", "", "", "", "", "", "")
		}
		if r := p.Status.Result; r != nil {
			ru, err := c.D.Resource(kube.Receipts).Namespace(*ns).Get(ctx, r.Receipt, metav1.GetOptions{})
			if err == nil {
				o, _ := kube.Decode[receipt.Receipt, struct{}](ru)
				rc := o.Spec
				row = append(row, f(r.Quality), f(rc.DeviceSeconds), f(rc.DeviceJoules), f(rc.HostCPUSeconds), f(rc.GCO2), f(rc.CI),
					f(rc.Started.Sub(p.Meta.CreationTimestamp.Time).Seconds()), strconv.FormatUint(rc.Seq, 10), "")
			}
		} else {
			row = append(row, "", "", "", "", "", "", "", "", p.Status.Message)
		}
		w.Write(row)
	}
	w.Flush()
	f2 := file
	f2.Close()
	log.Printf("wrote %s", *out)
}

func f(x float64) string { return strconv.FormatFloat(x, 'g', 6, 64) }
