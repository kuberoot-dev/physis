// Package kube is the thin layer between physis and the Kubernetes API: the
// resources are CRDs, read and written through the dynamic client, converted
// to the Go types of api/v1alpha1. Nothing here reads or writes Node objects
// (principle P7); the conformance suite checks that the RBAC physis is given
// has no access to them.
package kube

import (
	"context"
	"encoding/json"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	v1 "github.com/kuberoot-dev/physis/api/v1alpha1"
)

var (
	Substrates = schema.GroupVersionResource{Group: v1.Group, Version: v1.Version, Resource: "substrates"}
	Problems   = schema.GroupVersionResource{Group: v1.Group, Version: v1.Version, Resource: "problems"}
	Receipts   = schema.GroupVersionResource{Group: v1.Group, Version: v1.Version, Resource: "receipts"}
	Mandates   = schema.GroupVersionResource{Group: v1.Group, Version: v1.Version, Resource: "mandates"}
)

// Client wraps the dynamic client.
type Client struct {
	D dynamic.Interface
}

// New builds a client from in-cluster config or a kubeconfig path.
func New(kubeconfig string) (*Client, error) {
	var cfg *rest.Config
	var err error
	if kubeconfig == "" {
		cfg, err = rest.InClusterConfig()
	} else {
		cfg, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
	}
	if err != nil {
		return nil, err
	}
	cfg.QPS, cfg.Burst = 50, 100
	d, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}
	return &Client{D: d}, nil
}

// Object is a typed view of an unstructured object.
type Object[S, T any] struct {
	Meta   metav1.ObjectMeta `json:"metadata"`
	Spec   S                 `json:"spec"`
	Status T                 `json:"status,omitempty"`
}

// Decode converts an unstructured object into a typed one.
func Decode[S, T any](u *unstructured.Unstructured) (*Object[S, T], error) {
	b, err := json.Marshal(u.Object)
	if err != nil {
		return nil, err
	}
	o := &Object[S, T]{}
	if err := json.Unmarshal(b, o); err != nil {
		return nil, err
	}
	return o, nil
}

// Encode converts a typed object into an unstructured one of the given kind.
func Encode[S, T any](kind string, o *Object[S, T]) (*unstructured.Unstructured, error) {
	b, err := json.Marshal(o)
	if err != nil {
		return nil, err
	}
	m := map[string]any{}
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	m["apiVersion"] = v1.Group + "/" + v1.Version
	m["kind"] = kind
	return &unstructured.Unstructured{Object: m}, nil
}

type (
	Substrate = Object[v1.SubstrateSpec, v1.SubstrateStatus]
	Problem   = Object[v1.ProblemSpec, v1.ProblemStatus]
)

// ListSubstrates returns all substrates.
func (c *Client) ListSubstrates(ctx context.Context) ([]*Substrate, error) {
	l, err := c.D.Resource(Substrates).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	var out []*Substrate
	for i := range l.Items {
		s, err := Decode[v1.SubstrateSpec, v1.SubstrateStatus](&l.Items[i])
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, nil
}

// ListProblems returns problems in all namespaces.
func (c *Client) ListProblems(ctx context.Context) ([]*Problem, error) {
	l, err := c.D.Resource(Problems).Namespace("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	var out []*Problem
	for i := range l.Items {
		p, err := Decode[v1.ProblemSpec, v1.ProblemStatus](&l.Items[i])
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, nil
}

// UpdateProblemStatus writes the status subresource with optimistic
// concurrency: the resourceVersion in p.Meta must be current, so two agents
// claiming the same work cannot both succeed.
func (c *Client) UpdateProblemStatus(ctx context.Context, p *Problem) (*Problem, error) {
	u, err := Encode("Problem", p)
	if err != nil {
		return nil, err
	}
	r, err := c.D.Resource(Problems).Namespace(p.Meta.Namespace).UpdateStatus(ctx, u, metav1.UpdateOptions{})
	if err != nil {
		return nil, err
	}
	return Decode[v1.ProblemSpec, v1.ProblemStatus](r)
}

// ApplySubstrate creates or updates a substrate's spec, then its status.
func (c *Client) ApplySubstrate(ctx context.Context, s *Substrate) (*Substrate, error) {
	ri := c.D.Resource(Substrates)
	cur, err := ri.Get(ctx, s.Meta.Name, metav1.GetOptions{})
	if err == nil {
		s.Meta.ResourceVersion = cur.GetResourceVersion()
		u, err := Encode("Substrate", s)
		if err != nil {
			return nil, err
		}
		r, err := ri.Update(ctx, u, metav1.UpdateOptions{})
		if err != nil {
			return nil, err
		}
		s.Meta.ResourceVersion = r.GetResourceVersion()
	} else {
		u, err := Encode("Substrate", s)
		if err != nil {
			return nil, err
		}
		r, err := ri.Create(ctx, u, metav1.CreateOptions{})
		if err != nil {
			return nil, fmt.Errorf("create substrate: %w", err)
		}
		s.Meta.ResourceVersion = r.GetResourceVersion()
	}
	u, err := Encode("Substrate", s)
	if err != nil {
		return nil, err
	}
	r, err := ri.UpdateStatus(ctx, u, metav1.UpdateOptions{})
	if err != nil {
		return nil, err
	}
	return Decode[v1.SubstrateSpec, v1.SubstrateStatus](r)
}

// UpdateSubstrateStatus writes a substrate's status.
func (c *Client) UpdateSubstrateStatus(ctx context.Context, s *Substrate) (*Substrate, error) {
	u, err := Encode("Substrate", s)
	if err != nil {
		return nil, err
	}
	r, err := c.D.Resource(Substrates).UpdateStatus(ctx, u, metav1.UpdateOptions{})
	if err != nil {
		return nil, err
	}
	return Decode[v1.SubstrateSpec, v1.SubstrateStatus](r)
}

// GetSubstrate reads one substrate.
func (c *Client) GetSubstrate(ctx context.Context, name string) (*Substrate, error) {
	u, err := c.D.Resource(Substrates).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	return Decode[v1.SubstrateSpec, v1.SubstrateStatus](u)
}

// CreateReceipt stores a signed receipt. Receipts are never updated.
func (c *Client) CreateReceipt(ctx context.Context, ns, name string, r *v1.ReceiptSpec) error {
	o := &Object[v1.ReceiptSpec, struct{}]{Meta: metav1.ObjectMeta{Name: name, Namespace: ns}, Spec: *r}
	u, err := Encode("Receipt", o)
	if err != nil {
		return err
	}
	_, err = c.D.Resource(Receipts).Namespace(ns).Create(ctx, u, metav1.CreateOptions{})
	return err
}
