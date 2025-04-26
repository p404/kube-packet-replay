package cmd

import (
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "kube-packet-replay",
	Short: "Capture and replay network packets in Kubernetes pods",
	Long: `kube-packet-replay is a tool that leverages ephemeral containers 
to capture and replay network traffic in Kubernetes pods.
It can be used for debugging, testing, and troubleshooting network issues.`,
}

// Execute executes the root command
func Execute() error {
	return rootCmd.Execute()
}

func init() {
	// Global flags can be added here
	rootCmd.PersistentFlags().StringP("kubeconfig", "k", "", "path to the kubeconfig file (default is $HOME/.kube/config)")
	rootCmd.PersistentFlags().StringP("namespace", "n", "default", "kubernetes namespace")
}
