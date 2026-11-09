// Command physis-identity-addr prints the physis: address of a substrate's
// published public key (hex), for use with physis-agent -fetch.
package main

import (
	"fmt"
	"os"

	"github.com/kuberoot-dev/physis/pkg/identity"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: physis-identity-addr <public key, hex>")
		os.Exit(2)
	}
	id, err := identity.FromHex(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println(id.String())
}
