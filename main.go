package main

import (
	"fmt"
	"os"

	"github.com/p404/kube-packet-replay/cmd/cli"
)

func main() {
	if err := cli.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
