package capture

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/p404/kube-packet-replay/pkg/k8s"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// DebugImage is the container image used for network debugging
const DebugImage = "nicolaka/netshoot:latest"

// CapturePackets captures network packets from a Kubernetes pod
func CapturePackets(client *k8s.Client, namespace, podName, containerName, filterExpr, outputFile string, duration time.Duration) error {
	fmt.Printf("Capturing packets matching filter '%s' from pod %s/%s in namespace %s\n",
		filterExpr, podName, containerName, namespace)

	// Create a debug container name with timestamp to avoid collisions
	timestamp := time.Now().Unix()
	debugContainerName := fmt.Sprintf("debug-%s-%d", containerName, timestamp)
	if containerName == "" {
		debugContainerName = fmt.Sprintf("debug-%d", timestamp)
	}

	// Set up tcpdump command
	interfaceFlag := "any" // Capture on all interfaces

	// Construct tcpdump command with filter expression
	command := []string{
		"sh", "-c",
		fmt.Sprintf("tcpdump -i %s -w /tmp/capture.pcap '%s'",
			interfaceFlag, filterExpr),
	}

	// Create debug container
	if err := client.CreateDebugContainer(namespace, podName, containerName, DebugImage, command, debugContainerName); err != nil {
		return fmt.Errorf("failed to create debug container: %v", err)
	}

	fmt.Println("Debug container created. Waiting for it to be ready...")

	// Wait for the debug container to be ready
	err := waitForDebugContainerReady(client, namespace, podName, debugContainerName)
	if err != nil {
		return fmt.Errorf("failed to wait for debug container: %v", err)
	}

	fmt.Println("Debug container is ready. Starting packet capture...")

	if duration > 0 {
		fmt.Printf("Capture will run for %s...\n", duration)
	} else {
		fmt.Println("Press Ctrl+C to stop the capture...")
	}

	// Handle signals to stop capture gracefully
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	// Wait for signal or timeout
	if duration > 0 {
		select {
		case <-sigCh:
			fmt.Println("Interrupt received, stopping capture...")
		case <-time.After(duration):
			fmt.Println("Capture duration reached, stopping...")
		}
	} else {
		// Wait indefinitely until signal
		<-sigCh
		fmt.Println("Interrupt received, stopping capture...")
	}

	// Get available container names to help with targeting the right container
	availableContainers, err := getAvailableContainers(client, namespace, podName)
	if err != nil {
		fmt.Printf("Warning: Unable to get container list: %v\n", err)
	}

	// Try to find our debug container in the available containers list
	targetContainer := findDebugContainer(availableContainers, debugContainerName)
	
	// Stop tcpdump by executing a command to kill it
	killCmd := []string{"pkill", "tcpdump"}
	_, err = client.ExecInContainer(namespace, podName, targetContainer, killCmd)
	if err != nil {
		fmt.Printf("Warning: Failed to stop tcpdump cleanly: %v\n", err)
		
		// Try with original debug container name
		if targetContainer != debugContainerName {
			_, err = client.ExecInContainer(namespace, podName, debugContainerName, killCmd)
			if err != nil {
				fmt.Printf("Warning: Failed with original debug container name: %v\n", err)
			}
		}
		
		// Try with literal container names from available containers
		for _, c := range availableContainers {
			if strings.Contains(c, "debug") {
				_, err = client.ExecInContainer(namespace, podName, c, killCmd)
				if err == nil {
					targetContainer = c
					fmt.Printf("Successfully stopped tcpdump in container: %s\n", c)
					break
				}
			}
		}
	}

	// Wait for tcpdump to finish writing to the file
	time.Sleep(2 * time.Second)

	// Copy the capture file from the pod
	fmt.Println("Copying capture file from the pod...")

	// Read file from pod
	execCmd := []string{"cat", "/tmp/capture.pcap"}
	var output string
	
	// First try with the container that worked for killing tcpdump
	output, err = client.ExecInContainer(namespace, podName, targetContainer, execCmd)
	if err != nil {
		fmt.Printf("Warning: Failed to copy from target container: %v\n", err)
		
		// Try each container that has "debug" in the name
		success := false
		for _, c := range availableContainers {
			if strings.Contains(c, "debug") {
				output, err = client.ExecInContainer(namespace, podName, c, execCmd)
				if err == nil {
					fmt.Printf("Successfully copied file from container: %s\n", c)
					success = true
					break
				}
			}
		}
		
		if !success {
			// Also try with original container
			if containerName != "" {
				output, err = client.ExecInContainer(namespace, podName, containerName, execCmd)
				if err != nil {
					return fmt.Errorf("failed to read file from pod - tried all container options: %v", err)
				}
			} else {
				return fmt.Errorf("failed to read file from pod - tried all container options")
			}
		}
	}

	// Write file locally
	err = os.WriteFile(outputFile, []byte(output), 0644)
	if err != nil {
		return fmt.Errorf("failed to write file locally: %v", err)
	}

	fmt.Printf("Packet capture saved to %s\n", outputFile)
	return nil
}

// getAvailableContainers gets a list of available containers in a pod
func getAvailableContainers(client *k8s.Client, namespace, podName string) ([]string, error) {
	// This mimics the "kubectl get pod" command with container name extraction
	cmd := []string{"sh", "-c", "echo $HOSTNAME"}
	_, err := client.ExecInContainer(namespace, podName, "", cmd)
	
	// Extract container names from the error message if present
	if err != nil {
		errorMsg := err.Error()
		if strings.Contains(errorMsg, "choose one of:") {
			start := strings.Index(errorMsg, "[")
			end := strings.Index(errorMsg, "]")
			if start != -1 && end != -1 && end > start {
				containerList := errorMsg[start+1:end]
				containers := strings.Split(containerList, " ")
				var result []string
				for _, c := range containers {
					c = strings.TrimSpace(c)
					if c != "" {
						result = append(result, c)
					}
				}
				return result, nil
			}
		}
		return nil, err
	}
	
	// If no error, this is unusual but we'll return an empty list
	return []string{}, nil
}

// findDebugContainer tries to find our debug container in the available containers list
func findDebugContainer(containers []string, debugContainerName string) string {
	// First try exact match
	for _, c := range containers {
		if c == debugContainerName {
			return c
		}
	}
	
	// Then try partial match for debug containers
	for _, c := range containers {
		if strings.Contains(c, "debug") {
			return c
		}
	}
	
	// Return original name if no match found
	return debugContainerName
}

// waitForDebugContainerReady waits for the debug container to be ready
func waitForDebugContainerReady(client *k8s.Client, namespace, podName, containerName string) error {
	timeout := time.After(2 * time.Minute)
	tick := time.Tick(2 * time.Second)

	for {
		select {
		case <-timeout:
			return fmt.Errorf("timed out waiting for debug container to be ready")
		case <-tick:
			pod, err := client.ClientSet.CoreV1().Pods(namespace).Get(
				context.TODO(),
				podName,
				metav1.GetOptions{},
			)
			if err != nil {
				return fmt.Errorf("failed to get pod: %v", err)
			}

			// Check if the container is ready
			for _, cs := range pod.Status.EphemeralContainerStatuses {
				if cs.Name == containerName {
					if cs.State.Running != nil {
						return nil // Container is running
					}
				}
			}
		}
	}
}
