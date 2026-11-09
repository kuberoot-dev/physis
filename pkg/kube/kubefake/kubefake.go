// Package kubefake gives tests a kube.Client backed by an in-memory API
// server: the client-go fake dynamic client, taught the physis resources.
package kubefake

import (
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"

	"github.com/kuberoot-dev/physis/pkg/kube"
)

// New returns a client whose store starts with objs.
func New(objs ...runtime.Object) (*kube.Client, *dynamicfake.FakeDynamicClient) {
	lists := map[schema.GroupVersionResource]string{
		kube.Substrates:                         "SubstrateList",
		kube.Problems:                           "ProblemList",
		kube.Receipts:                           "ReceiptList",
		kube.Mandates:                           "MandateList",
		kube.Links:                              "LinkList",
		{Version: "v1", Resource: "configmaps"}: "ConfigMapList",
		{Version: "v1", Resource: "secrets"}:    "SecretList",
	}
	d := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), lists, objs...)
	return &kube.Client{D: d}, d
}
