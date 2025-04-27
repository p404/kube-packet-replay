package cli

import (
	"github.com/spf13/cobra"
)

// NewRootCommand creates the root command
func NewRootCommand() *cobra.Command {
	rootCmd := &cobra.Command{
		Use:   "kube-packet-replay",
		Short: "Capture and replay network packets in Kubernetes pods",
		Long: `kube-packet-replay is a tool that leverages ephemeral containers 
to capture and replay network traffic in Kubernetes pods.
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
