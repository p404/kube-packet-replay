package main

import (
	"fmt"
	"os"

	"github.com/p404/kube-packet-replay/cmd/cli"
)

// Version information set via ldflags
var (
	version   = "dev"
	commit    = "unknown"
	buildDate = "unknown"
)

func main() {
	cli.SetVersionInfo(version, commit, buildDate)

	if err := cli.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
