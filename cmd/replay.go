package cmd

import (
	"strings"

	"github.com/p404/kube-packet-replay/pkg/k8s"
	"github.com/p404/kube-packet-replay/pkg/replay"
	"github.com/spf13/cobra"
)

var replayCmd = &cobra.Command{
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

		// Create Kubernetes client
		k8sClient, err := k8s.NewClient(cmd)
		if err != nil {
			return err
		}

		// Replay packets
		return replay.ReplayPackets(k8sClient, namespace, podName, containerName, inputFile)
	},
}

func init() {
	replayCmd.Flags().StringP("file", "f", "", "PCAP file to replay (required)")
	replayCmd.MarkFlagRequired("file")
	rootCmd.AddCommand(replayCmd)
}
