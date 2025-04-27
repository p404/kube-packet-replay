package cli

import (
	"fmt"
	"strings"
	"time"

	"github.com/p404/kube-packet-replay/pkg/capture"
	"github.com/p404/kube-packet-replay/pkg/k8s"
	"github.com/spf13/cobra"
)

// NewCaptureCommand creates the capture command
func NewCaptureCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "capture [protocol-filter] [pod-name]/[container]",
		Short: "Capture network packets from a Kubernetes pod",
		Long: `Capture network packets from a Kubernetes pod using ephemeral containers.

The protocol-filter can be any valid tcpdump filter expression. You can filter by protocol, port, IP address, or combine them. For example, you can capture only UDP traffic on port 8125 by using 'udp 8125', or TCP traffic on port 80 with 'tcp 80'.

Common protocol filters include:
  - tcp           # Capture all TCP traffic
  - udp           # Capture all UDP traffic
  - icmp          # Capture ICMP (ping) traffic
  - udp 8125      # Capture all UDP traffic on port 8125
  - tcp 80        # Capture all TCP traffic on port 80
  - port 53       # Capture all traffic on port 53 (any protocol)
  - host 10.0.0.1 # Capture all traffic to/from 10.0.0.1

You can combine filters, e.g.:
  - tcp port 80 or tcp port 443
  - udp and not port 53`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			// Get filter
			filterExpr := args[0]

			// Parse pod and container
			podContainer := args[1]
			podContainerParts := strings.Split(podContainer, "/")
			podName := podContainerParts[0]
			containerName := ""
			if len(podContainerParts) > 1 {
				containerName = podContainerParts[1]
			}

			// Get namespace flag
			namespace, _ := cmd.Flags().GetString("namespace")

			// Get output file name flag
			outputFile, _ := cmd.Flags().GetString("output-file")
			if outputFile == "" {
				outputFile = fmt.Sprintf("%s.pcap", podName)
			}

			// Get duration flag
			durationStr, _ := cmd.Flags().GetString("duration")
			var duration time.Duration
			if durationStr != "" {
				var err error
				duration, err = time.ParseDuration(durationStr)
				if err != nil {
					return fmt.Errorf("invalid duration: %v", err)
				}
			}

			// Get kubeconfig flag
			kubeconfig, _ := cmd.Flags().GetString("kubeconfig")

			// Get verbose flag
			verbose, _ := cmd.Flags().GetBool("verbose")

			// Create Kubernetes client
			client, err := k8s.NewClient(kubeconfig)
			if err != nil {
				return fmt.Errorf("failed to create Kubernetes client: %v", err)
			}

			// Execute capture
			return capture.CapturePackets(client, namespace, podName, containerName, filterExpr, outputFile, duration, verbose)
		},
	}

	// Add flags
	cmd.Flags().StringP("namespace", "n", "default", "Kubernetes namespace")
	cmd.Flags().StringP("output-file", "o", "", "Output file name (default: <pod-name>.pcap)")
	cmd.Flags().StringP("duration", "d", "", "Duration of capture (e.g. 10s, 5m)")
	cmd.Flags().StringP("kubeconfig", "k", "", "Path to kubeconfig file")
	cmd.Flags().BoolP("verbose", "v", false, "Verbose output")

	return cmd
}
