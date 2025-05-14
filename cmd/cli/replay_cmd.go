package cli

import (
	"fmt"
	"strings"

	"github.com/p404/kube-packet-replay/pkg/k8s"
	"github.com/p404/kube-packet-replay/pkg/replay"
	"github.com/spf13/cobra"
)

// NewReplayCommand creates the replay command
func NewReplayCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "replay {pod|deployment|statefulset|daemonset} [resource-name] -f [pcap-file]",
		Short: "Replay packets into Kubernetes pods or higher-level resources",
		Long: `Replay network packets from a PCAP file into Kubernetes pods or higher-level resources using ephemeral containers.

You must specify the resource type followed by the resource name. For example:
  - pod: to target a single pod, e.g. 'pod my-pod-name'
  - deployment: to target all pods in a deployment, e.g. 'deployment nginx'
  - statefulset: to target all pods in a statefulset, e.g. 'statefulset postgres'
  - daemonset: to target all pods in a daemonset, e.g. 'daemonset monitoring-agent'
   
When using a higher-level resource type (deployment, statefulset, etc.), the tool will
replay packets to all associated pods.

Examples:
  - kube-packet-replay replay pod nginx -n default -f captured.pcap                     # Replay in a single pod
  - kube-packet-replay replay pod nginx --target-container=app -n default -f captured.pcap    # Replay in specific container
  - kube-packet-replay replay deployment nginx -n default -f captured.pcap              # Replay in all pods of a deployment
  - kube-packet-replay replay statefulset postgres -n default -f captured.pcap          # Replay in all pods of a statefulset

You can also configure replay options using the following flags:
  - --interface: Specify which network interface to replay packets on (default: lo)
  - --speed: Control replay speed multiplier (default: 1.0 = original speed)
  - --loop: Specify how many times to replay the capture (default: 1)`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			// Get the resource type (pod, deployment, statefulset, daemonset)
			resourceType := strings.ToLower(args[0])
			
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
			
			// Get the resource name
			resourceName := args[1]
			
			// Get the target container name from flag
			containerName, _ := cmd.Flags().GetString("target-container")

			// Get namespace flag
			namespace, _ := cmd.Flags().GetString("namespace")

			// Get input file name
			inputFile, _ := cmd.Flags().GetString("file")

			// Get kubeconfig flag
			kubeconfig, _ := cmd.Flags().GetString("kubeconfig")

			// Get replay options
			interface_, _ := cmd.Flags().GetString("interface")
			speed, _ := cmd.Flags().GetFloat64("speed")
			loop, _ := cmd.Flags().GetInt("loop")
			resourceBased, _ := cmd.Flags().GetBool("resource-based")

			// Create replay options
			opts := &replay.ReplayOptions{
				NetworkInterface: interface_,
				SpeedMultiplier: speed,
				LoopCount:       loop,
			}
			
			// Create Kubernetes client
			client, err := k8s.NewClient(kubeconfig)
			if err != nil {
				return fmt.Errorf("failed to create Kubernetes client: %v", err)
			}

			// Check if we should try to use resource-based replay
			if resourceBased {
				return replay.ReplayPacketsToResource(client, namespace, resourceName, containerName, inputFile, opts)
			}

			// Use the resource type specified in the command
			fmt.Printf("\nUsing specified resource type '%s' for '%s' in namespace '%s'...\n", resourceType, resourceName, namespace)
			
			// Handle different resource types
			var resourceInfo *k8s.ResourceInfo
			
			// Get pods based on the specified resource type
			resourceInfo, podErr := client.GetPodsFromResource(namespace, resourceName)
			if podErr != nil {
				return fmt.Errorf("failed to find '%s' resource named '%s': %v", resourceType, resourceName, podErr)
			}
			
			// Validate that the detected resource matches what was specified
			if resourceInfo.Type != k8sResourceType {
				return fmt.Errorf("resource '%s' was found but is a %s, not a %s as specified", 
					resourceName, string(resourceInfo.Type), resourceType)
			}
			
			// Log the resource detection results
			fmt.Printf("Resource detection successful: Found %s '%s' with %d pods\n", 
				string(resourceInfo.Type), resourceInfo.Name, len(resourceInfo.PodNames))
			
			// List the discovered pods
			fmt.Printf("\nPods in this %s:\n", string(resourceInfo.Type))
			for i, podName := range resourceInfo.PodNames {
				fmt.Printf("  %d. %s\n", i+1, podName)
			}
			fmt.Println()
			
			// Decide whether to use single pod or multi-pod replay based on resource type
			if resourceInfo.Type == k8s.ResourceTypePod {
				// Single pod replay
				fmt.Printf("Using single pod replay for %s\n", resourceName)
				return replay.ReplayPackets(client, namespace, resourceName, containerName, inputFile, opts)
			} else {
				// Multi-pod replay for higher-level resources
				fmt.Printf("Using multi-pod replay for all %d pods in %s '%s'\n", 
					len(resourceInfo.PodNames), string(resourceInfo.Type), resourceName)
				return replay.ReplayPacketsToResource(client, namespace, resourceName, containerName, inputFile, opts)
			}
		},
	}

	// Add flags
	cmd.Flags().StringP("namespace", "n", "default", "Kubernetes namespace")
	cmd.Flags().StringP("file", "f", "", "Input PCAP file")
	cmd.MarkFlagRequired("file")
	cmd.Flags().StringP("kubeconfig", "k", "", "Path to kubeconfig file")
	cmd.Flags().StringP("interface", "i", "lo", "Interface to replay on")
	cmd.Flags().Float64P("speed", "s", 1.0, "Replay speed multiplier")
	cmd.Flags().IntP("loop", "l", 1, "Number of times to replay the capture")
	cmd.Flags().BoolP("verbose", "v", false, "Verbose output")
	cmd.Flags().String("target-container", "", "Target specific container in the pod (optional)")
	cmd.Flags().BoolP("resource-based", "r", false, "Force resource-based replay, even for single pod resources")

	return cmd
}
