// Command stand starts a Kubernetes control plane with no nodes at all -- an
// etcd and a kube-apiserver from the envtest binaries -- installs the physis
// CRDs and writes a kubeconfig. physis runs against it as plain processes:
// the point of this stand is that the system needs no Node object, no kubelet
// and no container runtime to schedule and run work.
package main

import (
	"flag"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

func main() {
	assets := flag.String("assets", "", "directory with etcd and kube-apiserver")
	kubeconfig := flag.String("out", "out/stand/kubeconfig", "where to write the kubeconfig")
	flag.Parse()
	env := &envtest.Environment{BinaryAssetsDirectory: *assets, CRDDirectoryPaths: []string{"deploy/crds.yaml"}, ErrorIfCRDPathMissing: true}
	cfg, err := env.Start()
	if err != nil {
		log.Fatal(err)
	}
	user, err := env.ControlPlane.AddUser(envtest.User{Name: "physis-admin", Groups: []string{"system:masters"}}, cfg)
	if err != nil {
		log.Fatal(err)
	}
	kc, err := user.KubeConfig()
	if err != nil {
		log.Fatal(err)
	}
	os.MkdirAll(filepath.Dir(*kubeconfig), 0o755)
	if err := os.WriteFile(*kubeconfig, kc, 0o600); err != nil {
		log.Fatal(err)
	}
	log.Printf("control plane up at %s; kubeconfig %s", cfg.Host, *kubeconfig)
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
	env.Stop()
}
