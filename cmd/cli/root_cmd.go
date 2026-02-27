package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

// Version information set via ldflags at build time
var (
	version   = "dev"
	commit    = "unknown"
	buildDate = "unknown"
)

// SetVersionInfo sets the version information (called from main)
func SetVersionInfo(v, c, d string) {
	if v != "" {
		version = v
	}
	if c != "" {
		commit = c
	}
	if d != "" {
		buildDate = d
	}
}

// NewRootCommand creates the root command
func NewRootCommand() *cobra.Command {
	rootCmd := &cobra.Command{
		Use:   "kube-packet-replay",
		Short: "Capture and replay network packets in Kubernetes pods",
		Long: `Capture and replay network traffic in Kubernetes pods using ephemeral containers.
Works with pods, deployments, statefulsets, and daemonsets — no sidecars or restarts needed.`,
		Example: `  # Capture TCP traffic on port 80 from a deployment
  kube-packet-replay capture "tcp port 80" deployment nginx

  # Replay the captured traffic back into the pod
  kube-packet-replay replay pod nginx -f nginx.pcap.gz

  # Capture with a duration limit and custom namespace
  kube-packet-replay capture "udp port 53" pod coredns -n kube-system -d 30s`,
	}

	// Add subcommands
	rootCmd.AddCommand(NewCaptureCommand())
	rootCmd.AddCommand(NewReplayCommand())
	rootCmd.AddCommand(newVersionCommand())

	return rootCmd
}

// newVersionCommand creates the version subcommand
func newVersionCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("kube-packet-replay %s\n", version)
			fmt.Printf("  commit:     %s\n", commit)
			fmt.Printf("  built:      %s\n", buildDate)
		},
	}
}

// Execute executes the root command
func Execute() error {
	return NewRootCommand().Execute()
}
