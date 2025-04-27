package cli

import (
	"strings"

	"github.com/p404/kube-packet-replay/pkg/k8s"
	"github.com/p404/kube-packet-replay/pkg/replay"
	"github.com/spf13/cobra"
)

// NewReplayCommand creates the replay command
func NewReplayCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "replay [pod-name]/[container] -f [pcap-file]",
		Short: "Replay packets into a Kubernetes pod",
		Long: `Replay network packets from a PCAP file into a Kubernetes pod using ephemeral containers.
For example:
  kube-packet-replay replay nginx-pod/nginx -n default -f captured.pcap`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			// Parse pod and container
			podContainer := args[0]
			podContainerParts := strings.Split(podContainer, "/")
			podName := podContainerParts[0]
			containerName := ""
			if len(podContainerParts) > 1 {
				containerName = podContainerParts[1]
			}

			// Get namespace flag
			namespace, _ := cmd.Flags().GetString("namespace")

			// Get input file name
			inputFile, _ := cmd.Flags().GetString("file")

			// Get kubeconfig flag
			kubeconfig, _ := cmd.Flags().GetString("kubeconfig")

			// Create Kubernetes client
			client, err := k8s.NewClient(kubeconfig)
			if err != nil {
				return err
			}

			// Execute replay
			return replay.ReplayPackets(client, namespace, podName, containerName, inputFile)
		},
	}

	// Add flags
	cmd.Flags().StringP("namespace", "n", "default", "Kubernetes namespace")
	cmd.Flags().StringP("file", "f", "", "PCAP file to replay")
	cmd.MarkFlagRequired("file")
	cmd.Flags().StringP("kubeconfig", "k", "", "Path to kubeconfig file")

	return cmd
}
