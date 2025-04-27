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

	"bytes"
	"os/exec"
	"path/filepath"
)

// DebugImage is the container image used for network debugging
const DebugImage = "nicolaka/netshoot:latest"

// CapturePackets captures network packets from a Kubernetes pod
func CapturePackets(client *k8s.Client, namespace, podName, containerName, filterExpr, outputFile string, duration time.Duration, verbose bool) error {
	fmt.Printf("Capturing packets matching filter '%s' from pod %s/%s in namespace %s\n",
		filterExpr, podName, containerName, namespace)

	// Create a debug container name with timestamp to avoid collisions
	timestamp := time.Now().Unix()
	debugContainerName := fmt.Sprintf("debug-%s-%d", containerName, timestamp)
	if containerName == "" {
		debugContainerName = fmt.Sprintf("debug-%d", timestamp)
	}

	fmt.Printf("Creating debug container with name: %s in pod: %s\n", debugContainerName, podName)

	// Store the original container name for fallback file access
	origContainerName := containerName
	if origContainerName == "" {
		// Get the first app container from the pod if none specified
		pod, err := client.ClientSet.CoreV1().Pods(namespace).Get(
			context.TODO(),
			podName,
			metav1.GetOptions{},
		)
		if err == nil && len(pod.Spec.Containers) > 0 {
			origContainerName = pod.Spec.Containers[0].Name
			if verbose {
				fmt.Printf("Found primary container name: %s\n", origContainerName)
			}
		}
	}

	// Set up tcpdump command with a trap to keep the container running even after tcpdump finishes
	// This lets us retrieve the capture file even if tcpdump exits
	interfaceFlag := "any" // Capture on all interfaces

	// Construct tcpdump command with filter expression
	// Use a trap to keep the container running after tcpdump finishes
	tcpdumpCmd := fmt.Sprintf(`
trap 'echo "Received signal, but will keep running to allow file retrieval"; sleep 3600' TERM INT
echo "Starting packet capture..."
tcpdump -i %s -w /tmp/capture.pcap '%s'
echo "Tcpdump exited, keeping container alive for file retrieval"
sleep 3600
`, interfaceFlag, filterExpr)

	command := []string{
		"sh", "-c",
		tcpdumpCmd,
	}
	
	if verbose {
		fmt.Printf("Debug container command:\n%s\n", tcpdumpCmd)
	}

	// Create debug container
	if err := client.CreateDebugContainer(namespace, podName, containerName, DebugImage, command, debugContainerName); err != nil {
		return fmt.Errorf("failed to create debug container: %v", err)
	}

	fmt.Printf("Debug container '%s' created. Waiting for it to be ready...\n", debugContainerName)

	// Wait for the debug container to be ready
	err := waitForDebugContainerReady(client, namespace, podName, debugContainerName)
	if err != nil {
		return fmt.Errorf("failed to wait for debug container: %v", err)
	}

	fmt.Printf("Debug container '%s' is ready. Starting packet capture...\n", debugContainerName)

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

	// Get list of containers BEFORE attempting to kill tcpdump
	containers := extractContainersFromPod(client, namespace, podName)
	if verbose {
		fmt.Printf("Available containers in pod: %v\n", containers)
	}
	
	// Find our debug container in the list
	var debugContainer string
	for _, c := range containers {
		if strings.Contains(c, "debug-"+containerName) {
			if verbose {
				fmt.Printf("Found matching debug container: %s\n", c)
			}
			// Prefer the most recently created container (highest timestamp)
			if debugContainer == "" || strings.Compare(c, debugContainer) > 0 {
				debugContainer = c
			}
		}
	}
	
	if debugContainer != "" {
		if verbose {
			fmt.Printf("Using debug container: %s\n", debugContainer)
		}
	} else {
		if verbose {
			fmt.Printf("No debug container found in pod list, trying original name: %s\n", debugContainerName)
		}
		debugContainer = debugContainerName
	}

	// CRITICAL: Try to copy the file BEFORE killing tcpdump since killing it may terminate the container
	fmt.Println("Attempting to copy capture file before stopping tcpdump...")
	
	// Try to copy from debug container first
	var output string
	var fileSuccess bool
	
	// First try using the debug container to get the file
	output, err = tryExecCat(client, namespace, podName, debugContainer, "/tmp/capture.pcap", verbose)
	if err == nil {
		// Successfully retrieved the file
		fileSuccess = true
		fmt.Printf("Successfully retrieved capture data (%d bytes) from debug container.\n", len(output))
	} else if verbose {
		fmt.Printf("Could not retrieve capture from debug container: %v\n", err)
	}
	
	// Now we can try to stop tcpdump
	fmt.Printf("Attempting to stop tcpdump in debug container '%s'...\n", debugContainer)
	
	tryKillTcpdump(client, namespace, podName, debugContainer, verbose)
	
	// Wait for tcpdump to flush the file
	fmt.Println("Waiting for packet capture file to be flushed to disk...")
	time.Sleep(3 * time.Second)
	
	// If we haven't retrieved the file yet, try again after stopping tcpdump
	if !fileSuccess {
		// Copy the capture file from the pod
		fmt.Println("Copying capture file from the pod...")
		
		// First try using the original container, which is most likely still running
		if origContainerName != "" {
			if verbose {
				fmt.Printf("Attempting to copy the capture file from the original container: %s\n", origContainerName)
			}
			
			// Try direct cat approach first - most reliable
			output, err = tryExecCat(client, namespace, podName, origContainerName, "/tmp/capture.pcap", verbose)
			if err == nil {
				fileSuccess = true
				fmt.Printf("Successfully retrieved capture data (%d bytes) from original container.\n", len(output))
			} else if verbose {
				fmt.Printf("Failed to cat file from original container: %v\n", err)
			}
		}
		
		// If still not successful, try other containers
		if !fileSuccess {
			// Try with the debug container again (may still be running)
			output, err = tryExecCat(client, namespace, podName, debugContainer, "/tmp/capture.pcap", verbose)
			if err == nil {
				fileSuccess = true
				fmt.Printf("Successfully retrieved capture data (%d bytes) from debug container (after stopping tcpdump).\n", len(output))
			} else if verbose {
				fmt.Printf("Failed to cat file from debug container after stopping tcpdump: %v\n", err)
			}
			
			// Try each container
			if !fileSuccess {
				// First try each app container in the pod
				for _, c := range containers {
					// Skip debug containers as we already tried those
					if !strings.Contains(c, "debug") {
						if verbose {
							fmt.Printf("Trying to copy capture file from app container: %s\n", c)
						}
						output, err = tryExecCat(client, namespace, podName, c, "/tmp/capture.pcap", verbose)
						if err == nil {
							fmt.Printf("Successfully copied file from container: %s\n", c)
							fileSuccess = true
							break
						}
					}
				}
				
				// If still no success, try debug containers
				if !fileSuccess {
					for _, c := range containers {
						if strings.Contains(c, "debug") && c != debugContainer {
							if verbose {
								fmt.Printf("Trying to copy capture file from debug container: %s\n", c)
							}
							output, err = tryExecCat(client, namespace, podName, c, "/tmp/capture.pcap", verbose)
							if err == nil {
								fmt.Printf("Successfully copied file from container: %s\n", c)
								fileSuccess = true
								break
							}
						}
					}
				}
			}
		}
	}
	
	if !fileSuccess {
		return fmt.Errorf("failed to read capture file from any container in the pod. Please try using --duration flag instead of interrupting with Ctrl+C.")
	}

	// Write file locally
	fmt.Printf("Successfully retrieved capture data (%d bytes).\n", len(output))
	fmt.Printf("Saving packet capture to %s...\n", outputFile)
	
	// Ensure directory exists
	dir := filepath.Dir(outputFile)
	if dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0755); err != nil {
			fmt.Printf("Warning: Could not create directory %s: %v\n", dir, err)
		}
	}
	
	err = os.WriteFile(outputFile, []byte(output), 0644)
	if err == nil {
		fileInfo, err := os.Stat(outputFile)
		if err == nil {
			fmt.Printf("Packet capture saved to %s (%d bytes)\n", outputFile, fileInfo.Size())
		} else {
			fmt.Printf("Packet capture saved to %s\n", outputFile)
		}
		return nil
	} else {
		fmt.Printf("ERROR: Failed to write file locally: %v\n", err)
		
		// Try with absolute path as fallback
		absPath, err2 := filepath.Abs(outputFile)
		if err2 == nil && absPath != outputFile {
			fmt.Printf("Trying with absolute path: %s\n", absPath)
			err = os.WriteFile(absPath, []byte(output), 0644)
			if err == nil {
				fmt.Printf("Packet capture saved to %s\n", absPath)
				return nil
			} else {
				fmt.Printf("ERROR: Failed to write file with absolute path: %v\n", err)
			}
		}
		
		// Try with home directory as fallback
		homeDir, err := os.UserHomeDir()
		if err == nil {
			homeFile := filepath.Join(homeDir, "captured.pcap")
			fmt.Printf("Trying to save to home directory: %s\n", homeFile)
			err = os.WriteFile(homeFile, []byte(output), 0644)
			if err == nil {
				fmt.Printf("Packet capture saved to %s\n", homeFile)
				return nil
			} else {
				fmt.Printf("ERROR: Failed to write file to home directory: %v\n", err)
			}
		}
		
		// Last resort, try current directory
		currentDir := "./captured.pcap"
		fmt.Printf("Trying to save to current directory: %s\n", currentDir)
		err = os.WriteFile(currentDir, []byte(output), 0644)
		if err == nil {
			fmt.Printf("Packet capture saved to %s\n", currentDir)
			return nil
		} else {
			fmt.Printf("ERROR: Failed to write file to current directory: %v\n", err)
			return fmt.Errorf("failed to write file to any location: %v", err)
		}
	}
}

// tryKillTcpdump attempts to kill tcpdump using direct kubectl exec
func tryKillTcpdump(client *k8s.Client, namespace, podName, containerName string, verbose bool) bool {
	kubectlCmd := fmt.Sprintf("kubectl exec -n %s -c %s %s -- pkill tcpdump", 
		namespace, containerName, podName)
	
	cmd := exec.Command("sh", "-c", kubectlCmd)
	if client.ConfigPath != "" {
		kubectlCmd = fmt.Sprintf("kubectl --kubeconfig %s exec -n %s -c %s %s -- pkill tcpdump", 
			client.ConfigPath, namespace, containerName, podName)
		cmd = exec.Command("sh", "-c", kubectlCmd)
	}
	
	if verbose {
		fmt.Printf("Executing: %s\n", kubectlCmd)
	}
	
	err := cmd.Run()
	return err == nil
}

// tryExecCat attempts to cat a file using direct kubectl exec
func tryExecCat(client *k8s.Client, namespace, podName, containerName, filePath string, verbose bool) (string, error) {
	kubectlCmd := fmt.Sprintf("kubectl exec -n %s -c %s %s -- cat %s", 
		namespace, containerName, podName, filePath)
	
	cmd := exec.Command("sh", "-c", kubectlCmd)
	if client.ConfigPath != "" {
		kubectlCmd = fmt.Sprintf("kubectl --kubeconfig %s exec -n %s -c %s %s -- cat %s", 
			client.ConfigPath, namespace, containerName, podName, filePath)
		cmd = exec.Command("sh", "-c", kubectlCmd)
	}
	
	if verbose {
		fmt.Printf("Executing: %s\n", kubectlCmd)
	}
	
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	
	err := cmd.Run()
	if err != nil {
		if verbose {
			fmt.Printf("Command failed: %v\nStderr: %s\n", err, stderr.String())
		}
		return "", fmt.Errorf("failed to cat file: %v", err)
	}
	
	return stdout.String(), nil
}

// tryKubectlCp attempts to copy a file using kubectl cp
func tryKubectlCp(client *k8s.Client, namespace, podName, containerName, outputFile string, verbose bool) (string, error) {
	// Correct kubectl cp syntax: kubectl cp -n NAMESPACE POD_NAME:PATH -c CONTAINER_NAME LOCAL_PATH
	kubectlCmd := fmt.Sprintf("kubectl cp -n %s %s:/tmp/capture.pcap -c %s %s", 
		namespace, podName, containerName, outputFile)
	
	cmd := exec.Command("sh", "-c", kubectlCmd)
	if client.ConfigPath != "" {
		kubectlCmd = fmt.Sprintf("kubectl --kubeconfig %s cp -n %s %s:/tmp/capture.pcap -c %s %s", 
			client.ConfigPath, namespace, podName, containerName, outputFile)
		cmd = exec.Command("sh", "-c", kubectlCmd)
	}
	
	if verbose {
		fmt.Printf("Executing: %s\n", kubectlCmd)
	}
	
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	
	err := cmd.Run()
	if err != nil {
		if verbose {
			fmt.Printf("Command failed: %v\nStderr: %s\n", err, stderr.String())
		}
		return "", fmt.Errorf("failed to copy file: %v", err)
	}
	
	// Verify the file actually exists
	if _, err := os.Stat(outputFile); os.IsNotExist(err) {
		if verbose {
			fmt.Printf("kubectl cp reported success but file doesn't exist at %s\n", outputFile)
		}
		return "", fmt.Errorf("kubectl cp did not create the output file")
	}
	
	// Check file size
	fileInfo, err := os.Stat(outputFile)
	if err == nil {
		if fileInfo.Size() == 0 {
			if verbose {
				fmt.Printf("Warning: Captured file is empty (0 bytes)\n")
			}
			return "", fmt.Errorf("captured file is empty")
		} else {
			if verbose {
				fmt.Printf("Successfully copied file, size: %d bytes\n", fileInfo.Size())
			}
		}
	}
	
	return outputFile, nil
}

// extractContainersFromPod gets container names from a pod by parsing error messages
func extractContainersFromPod(client *k8s.Client, namespace, podName string) []string {
	cmd := []string{"sh", "-c", "echo 'Getting container list'"}
	_, err := client.ExecInContainer(namespace, podName, "", cmd)
	
	if err != nil {
		errorMsg := err.Error()
		// Check if the error contains container list
		if strings.Contains(errorMsg, "choose one of:") {
			start := strings.Index(errorMsg, "[")
			end := strings.Index(errorMsg, "]")
			if start != -1 && end != -1 && end > start {
				containerList := errorMsg[start+1:end]
				// Split by spaces or commas
				containers := strings.FieldsFunc(containerList, func(r rune) bool {
					return r == ' ' || r == ','
				})
				
				var result []string
				for _, c := range containers {
					c = strings.TrimSpace(c)
					if c != "" {
						result = append(result, c)
					}
				}
				return result
			}
		}
	}
	
	// If we can't extract containers, return an empty list
	return []string{}
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
