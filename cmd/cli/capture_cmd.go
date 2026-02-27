package cli

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/p404/kube-packet-replay/pkg/capture"
	"github.com/p404/kube-packet-replay/pkg/k8s"
	"github.com/p404/kube-packet-replay/pkg/output"
	"github.com/p404/kube-packet-replay/pkg/validation"
	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// NewCaptureCommand creates the capture command
func NewCaptureCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "capture [protocol-filter] {pod|deployment|statefulset|daemonset} [resource-name]",
		Short: "Capture network packets from a Kubernetes pod or higher-level resource",
		Long: `Capture network packets from a Kubernetes pod or higher-level resource using ephemeral containers.

The protocol-filter can be any valid tcpdump filter expression. You can filter by protocol, port, IP address, or combine them.

You must specify the resource type followed by the resource name. For example:
  - pod: to target a single pod, e.g. 'pod my-pod-name'
  - deployment: to target all pods in a deployment, e.g. 'deployment nginx'
  - statefulset: to target all pods in a statefulset, e.g. 'statefulset postgres'
  - daemonset: to target all pods in a daemonset, e.g. 'daemonset monitoring-agent'

When using a higher-level resource type (deployment, statefulset, etc.), the tool will
capture packets from all associated pods.

Examples:
  - kube-packet-replay capture "tcp port 80" deployment nginx         # Capture from all pods in a deployment
  - kube-packet-replay capture udp pod mypod --target-container=nginx # Capture UDP from a specific container in a pod
  - kube-packet-replay capture icmp statefulset postgres            # Capture ICMP from all pods in a statefulset
  - kube-packet-replay capture "host 10.0.0.1" daemonset monitoring # Capture from all pods in a daemonset

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
		Args: cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			out := output.Default()

			// Get filter
			filterExpr := args[0]

			// Validate filter expression for shell safety
			if err := validation.ValidateFilterExpression(filterExpr); err != nil {
				return err
			}

			// Get the resource type (pod, deployment, statefulset, daemonset)
			resourceType := strings.ToLower(args[1])

			// Validate resource type
			validResourceTypes := map[string]k8s.ResourceType{
				"pod":         k8s.ResourceTypePod,
				"deployment":  k8s.ResourceTypeDeployment,
				"statefulset": k8s.ResourceTypeStatefulSet,
				"daemonset":   k8s.ResourceTypeDaemonSet,
			}

			k8sResourceType, ok := validResourceTypes[resourceType]
			if !ok {
				return fmt.Errorf("invalid resource type '%s'. Must be one of: pod, deployment, statefulset, daemonset", resourceType)
			}

			// Get the resource name(s) - supports comma-separated list
			resourceNames := strings.Split(args[2], ",")

			// Trim whitespace and validate each resource name
			for i := range resourceNames {
				resourceNames[i] = strings.TrimSpace(resourceNames[i])
				if err := validation.ValidateKubernetesName(resourceNames[i], resourceType); err != nil {
					return err
				}
			}

			// Get the target container name from flag
			containerName, _ := cmd.Flags().GetString("target-container")

			// Get namespace flag and validate
			namespace, _ := cmd.Flags().GetString("namespace")
			if err := validation.ValidateNamespace(namespace); err != nil {
				return err
			}

			// Get output file name flag and validate
			outputFile, _ := cmd.Flags().GetString("output-file")
			if err := validation.ValidateFilePath(outputFile); err != nil {
				return err
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

			// Get debug image override
			debugImage, _ := cmd.Flags().GetString("image")
			if debugImage != "" {
				capture.SetDebugImage(debugImage)
			}

			// Create Kubernetes client
			client, err := k8s.NewClient(kubeconfig)
			if err != nil {
				return fmt.Errorf("failed to create Kubernetes client: %v", err)
			}

			// Get the multi-pod flag
			resourceBased, _ := cmd.Flags().GetBool("resource-based")

			// Handle multiple resources if specified
			if len(resourceNames) > 1 && k8sResourceType != k8s.ResourceTypePod {
				out.Print("\nProcessing %d %ss: %s\n\n",
					len(resourceNames),
					resourceType,
					strings.Join(resourceNames, ", "))

				// Process each resource
				hasErrors := false
				for _, resourceName := range resourceNames {
					out.Print("\n=== Capturing from %s: %s ===\n", resourceType, resourceName)
					err = capture.CapturePacketsFromResource(client, namespace, resourceName, containerName,
						filterExpr, outputFile, duration, verbose)
					if err != nil {
						out.Error("Error capturing from %s '%s': %v", resourceType, resourceName, err)
						hasErrors = true
					}
				}

				if hasErrors {
					return fmt.Errorf("some captures failed, check output for details")
				}
				return nil
			}

			// Single resource name or pod
			resourceName := resourceNames[0]

			// Check if we should try to detect resource type
			if resourceBased {
				// Use the multi-capture functionality for a single resource
				return capture.CapturePacketsFromResource(client, namespace, resourceName, containerName,
					filterExpr, outputFile, duration, verbose)
			}

			// Create a ResourceInfo struct with the explicitly provided resource type
			var resourceInfo *k8s.ResourceInfo

			if k8sResourceType == k8s.ResourceTypePod {
				// For pod type, just verify the pod exists
				pod, podErr := client.ClientSet.CoreV1().Pods(namespace).Get(context.Background(), resourceName, metav1.GetOptions{})
				if podErr != nil {
					return fmt.Errorf("pod '%s' not found in namespace '%s': %v", resourceName, namespace, podErr)
				}

				// Create resource info for a single pod
				resourceInfo = &k8s.ResourceInfo{
					Type:     k8s.ResourceTypePod,
					Name:     pod.Name,
					PodNames: []string{pod.Name},
				}
			} else {
				// For other resource types, use the GetPodsFromResource helper
				resourceInfo, err = client.GetPodsFromResource(namespace, resourceName)

				// Validate that the detected resource type matches what was specified
				if err == nil && resourceInfo.Type != k8sResourceType {
					return fmt.Errorf("resource '%s' was found but is a %s, not a %s as specified",
						resourceName, string(resourceInfo.Type), string(k8sResourceType))
				}
			}

			if err != nil {
				return err
			}

			out.Info("Found %s '%s' with %d pod(s)",
				string(resourceInfo.Type), resourceInfo.Name, len(resourceInfo.PodNames))

			// Decide whether to use single pod or multi-pod capture based on resource type
			if resourceInfo.Type == k8s.ResourceTypePod {
				return capture.CapturePackets(client, namespace, resourceName, containerName,
					filterExpr, outputFile, duration, verbose)
			}
			// Multi-pod capture for higher-level resources
			return capture.CapturePacketsFromResource(client, namespace, resourceName, containerName,
				filterExpr, outputFile, duration, verbose)
		},
	}

	// Add flags
	cmd.Flags().StringP("namespace", "n", "default", "Kubernetes namespace")
	cmd.Flags().StringP("output-file", "o", "", "Output file name (default: <resourceName>.pcap.gz)")
	cmd.Flags().StringP("duration", "d", "", "Capture duration (e.g. 30s, 5m, 1h)")
	cmd.Flags().StringP("kubeconfig", "k", "", "Path to kubeconfig file")
	cmd.Flags().BoolP("verbose", "v", false, "Enable verbose output")
	cmd.Flags().Bool("resource-based", false, "Force multi-pod capture even for single pod")
	cmd.Flags().String("target-container", "", "Target specific container in the pod (optional)")
	cmd.Flags().String("image", "", "Override debug container image (default: nicolaka/netshoot:v0.13)")

	return cmd
}
