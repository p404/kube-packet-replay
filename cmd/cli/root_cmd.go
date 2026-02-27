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
		Short: "Capture and replay network packets in Kubernetes pods and higher-level resources",
		Long: `kube-packet-replay is a tool that leverages ephemeral containers
to capture and replay network traffic in Kubernetes pods and higher-level resources.

Key Features:
  - Works with pods, deployments, statefulsets, and daemonsets
  - Automatic resource type detection - just provide the resource name
  - Multi-pod capture and replay for higher-level resources
  - Configurable packet filters using standard tcpdump syntax
  - Compressed packet capture files
  - Detailed, color-coded output

It can be used for debugging, testing, and troubleshooting network issues.`,
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
