package cli

import (
	"github.com/spf13/cobra"
)

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
	
	return rootCmd
}

// Execute executes the root command
func Execute() error {
	return NewRootCommand().Execute()
}
