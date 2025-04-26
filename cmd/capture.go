package cmd

import (
	"fmt"
	"strings"
	"time"

	"github.com/p404/kube-packet-replay/pkg/capture"
	"github.com/p404/kube-packet-replay/pkg/k8s"
	"github.com/spf13/cobra"
)

var captureCmd = &cobra.Command{
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
  - udp and not port 53

Filtering by Receiving vs. Sending Traffic:
  - To capture only incoming (received) traffic, filter by destination IP or port using 'dst'.
    - Example: dst port 80        # Traffic received on port 80
    - Example: tcp and dst port 443 # Incoming TCP traffic on port 443
    - Example: dst host 10.0.0.1  # Traffic received to 10.0.0.1
  - To capture only outgoing (sent) traffic, filter by source IP or port using 'src'.
    - Example: src port 80        # Traffic sent from port 80
    - Example: udp and src port 8125 # Outgoing UDP traffic from port 8125
    - Example: src host 10.0.0.1  # Traffic sent from 10.0.0.1
  - To capture traffic sent TO a specific port (e.g., traffic your application sends to StatsD on UDP port 8125):
    - Example: dst port 8125 and udp # Traffic sent to UDP port 8125

Filtering Traffic in Multi-Container Pods:
  When you have multiple containers in a pod, all containers share the same network namespace.
  To filter traffic for a specific container, simply specify the container name after the pod name:
    - Example: pod-name/container-name
  This injects the debug container with the target container's process namespace.

To see all supported protocols and filter syntax, refer to the tcpdump documentation: https://www.tcpdump.org/manpages/pcap-filter.7.html
Or run 'man pcap-filter' on your system if tcpdump is installed.

Examples:
  # Capture UDP packets on port 8125
  kube-packet-replay capture "udp 8125" nginx-pod/nginx -n default
  
  # Capture only incoming TCP traffic on port 443
  kube-packet-replay capture "tcp and dst port 443" web-pod/web -n default
  
  # Capture only outgoing UDP traffic from port 8125
  kube-packet-replay capture "udp and src port 8125" metrics-pod/agent -n default
  
  # Capture traffic an application sends to StatsD (UDP port 8125)
  kube-packet-replay capture "dst port 8125 and udp" app-pod/app -n default
  
  # Capture traffic from a specific container in a multi-container pod
  kube-packet-replay capture "tcp" multi-pod/container1 -n default
  
  # Capture TCP packets on port 80
  kube-packet-replay capture "tcp 80" web-pod/web -n default
  
  # Capture all HTTP traffic
  kube-packet-replay capture "tcp port 80 or tcp port 443" app-pod/app -n default
  
  # Capture ICMP traffic for 5 minutes
  kube-packet-replay capture "icmp" monitoring-pod/agent -n default --minutes 5`,
	Args: cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		// Get tcpdump filter expression
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

		// Get optional output filename
		outputFile, _ := cmd.Flags().GetString("output")
		if outputFile == "" {
			outputFile = fmt.Sprintf("%s.pcap", podName)
		}

		// Get capture duration
		minutes, _ := cmd.Flags().GetInt("minutes")
		var duration time.Duration
		if minutes > 0 {
			duration = time.Duration(minutes) * time.Minute
		} else {
			// Use the duration flag if minutes not specified
			duration, _ = cmd.Flags().GetDuration("duration")
		}

		// Create Kubernetes client
		k8sClient, err := k8s.NewClient(cmd)
		if err != nil {
			return err
		}

		// Capture packets
		return capture.CapturePackets(k8sClient, namespace, podName, containerName, filterExpr, outputFile, duration)
	},
}

func init() {
	captureCmd.Flags().StringP("output", "o", "", "output file name (default: pod-name.pcap)")
	captureCmd.Flags().DurationP("duration", "d", 0, "duration of capture (e.g. 30s, 5m, 1h), 0 means capture until interrupted")
	captureCmd.Flags().IntP("minutes", "m", 0, "duration of capture in minutes (overrides --duration if specified)")
	rootCmd.AddCommand(captureCmd)
}
