// Command physis-dra-bridge turns devices published through Kubernetes
// Dynamic Resource Allocation into physis substrates. It reads every
// ResourceSlice, maps each device it recognises to a Substrate (see package
// dra) and keeps the substrate's spec in step with the slice. It never writes
// status: a bridged substrate becomes live only when an agent serves it
// (physis-agent -adopt), so a described but unserved device is never planned.
package main

import (
	"context"
	"flag"
	"log"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"

	v1 "github.com/kuberoot-dev/physis/api/v1alpha1"
	"github.com/kuberoot-dev/physis/pkg/dra"
	"github.com/kuberoot-dev/physis/pkg/kube"
)

var resourceSlices = schema.GroupVersionResource{Group: "resource.k8s.io", Version: "v1", Resource: "resourceslices"}

func main() {
	kubeconfig := flag.String("kubeconfig", "", "kubeconfig (empty: in-cluster)")
	zone := flag.String("zone", "", "zone of devices that do not state one")
	operator := flag.String("operator", "", "operator of devices that do not state one")
	jurisdiction := flag.String("jurisdiction", "", "jurisdiction of devices that do not state one")
	every := flag.Duration("interval", 10*time.Second, "how often to read the slices")
	once := flag.Bool("once", false, "reconcile once and exit")
	flag.Parse()
	c, err := kube.New(*kubeconfig)
	if err != nil {
		log.Fatal(err)
	}
	def := dra.Defaults{Zone: *zone, Operator: *operator, Jurisdiction: *jurisdiction}
	for {
		if err := reconcile(context.Background(), c, def); err != nil {
			log.Printf("reconcile: %v", err)
		}
		if *once {
			return
		}
		time.Sleep(*every)
	}
}

func reconcile(ctx context.Context, c *kube.Client, def dra.Defaults) error {
	l, err := c.D.Resource(resourceSlices).List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}
	for i := range l.Items {
		for _, d := range dra.Devices(l.Items[i].Object) {
			spec, ok, err := dra.ToSubstrate(d, def)
			if err != nil {
				log.Printf("%v", err)
				continue
			}
			if !ok {
				continue
			}
			if err := apply(ctx, c, d, spec); err != nil {
				log.Printf("device %s/%s: %v", d.Pool, d.Name, err)
			}
		}
	}
	return nil
}

// apply creates the substrate or updates its spec; status belongs to the
// agent that serves it and is left alone.
func apply(ctx context.Context, c *kube.Client, d dra.Device, spec v1.SubstrateSpec) error {
	name := dra.SubstrateName(d)
	source := "dra:" + d.Driver + "/" + d.Pool + "/" + d.Name
	ri := c.D.Resource(kube.Substrates)
	cur, err := ri.Get(ctx, name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		o := &kube.Substrate{Spec: spec}
		o.Meta.Name = name
		o.Meta.Annotations = map[string]string{dra.SourceAnnotation: source}
		u, err := kube.Encode("Substrate", o)
		if err != nil {
			return err
		}
		delete(u.Object, "status")
		if _, err := ri.Create(ctx, u, metav1.CreateOptions{}); err != nil {
			return err
		}
		log.Printf("substrate %s from %s (%s, zone %s)", name, source, spec.Physics, spec.Zone)
		return nil
	}
	if err != nil {
		return err
	}
	if cur.GetAnnotations()[dra.SourceAnnotation] != source {
		log.Printf("substrate %s exists and was not made from %s; left alone", name, source)
		return nil
	}
	o, err := kube.Decode[v1.SubstrateSpec, v1.SubstrateStatus](cur)
	if err != nil {
		return err
	}
	o.Spec = spec
	u, err := kube.Encode("Substrate", o)
	if err != nil {
		return err
	}
	_, err = ri.Update(ctx, u, metav1.UpdateOptions{})
	return err
}
