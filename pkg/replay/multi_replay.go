package replay

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/p404/kube-packet-replay/pkg/cli"
	"github.com/p404/kube-packet-replay/pkg/k8s"
)

// ReplayResult represents the result of replaying packets to a single pod
type ReplayResult struct {
	PodName       string
	ContainerName string
	Success       bool
	Error         error
	PacketsReplayed int
	BytesReplayed   int
}

// ReplayPacketsToResource replays network packets to all pods that belong to a resource
// (Deployment, StatefulSet, DaemonSet, or individual Pod)
func ReplayPacketsToResource(client *k8s.Client, namespace, resourceName, containerName, 
	inputFile string, opts *ReplayOptions) error {
	
	// Show starting message with date
	currentTime := time.Now().Format("2006-01-02 15:04:05")
	fmt.Printf("\n%s %s\n\n", 
		cli.Colorize(cli.ColorBold, "KUBE-PACKET-REPLAY RESOURCE REPLAY STARTED AT:"), 
		cli.Colorize(cli.ColorBlue, currentTime))
	
	// Step 1: Discover resource and pods
	fmt.Println(cli.Step(1, "Discovering Kubernetes resources"))

	// Find the resource and its associated pods
	resourceInfo, err := client.GetPodsFromResource(namespace, resourceName)
	if err != nil {
		return fmt.Errorf("resource discovery failed: %v", err)
	}
	
	// Verify we found pods
	if len(resourceInfo.PodNames) == 0 {
		return fmt.Errorf("no pods found for %s '%s' in namespace '%s'", 
			string(resourceInfo.Type), resourceInfo.Name, namespace)
	}
	
	// Display resource information
	fmt.Printf("  %s: %s\n", 
		cli.Colorize(cli.ColorBlue, "Resource Type"), 
		cli.Colorize(cli.ColorBold, string(resourceInfo.Type)))
	fmt.Printf("  %s: %s\n", 
		cli.Colorize(cli.ColorBlue, "Resource Name"), 
		cli.Colorize(cli.ColorBold, resourceInfo.Name))
	fmt.Printf("  %s: %s\n", 
		cli.Colorize(cli.ColorBlue, "Namespace"), 
		cli.Colorize(cli.ColorBold, namespace))
	fmt.Printf("  %s: %s\n", 
		cli.Colorize(cli.ColorBlue, "Pods Found"), 
		cli.Colorize(cli.ColorBold, fmt.Sprintf("%d", len(resourceInfo.PodNames))))
	
	// List the pods
	fmt.Printf("  %s:\n", cli.Colorize(cli.ColorBlue, "Target Pods"))
	for i, podName := range resourceInfo.PodNames {
		fmt.Printf("    %d. %s\n", i+1, cli.Colorize(cli.ColorBold, podName))
	}

	// Get file information for user feedback
	fileStat, err := os.Stat(inputFile)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("input file %s does not exist", inputFile)
		}
		return fmt.Errorf("failed to access input file: %v", err)
	}
	
	// Display file information
	fmt.Printf("  %s: %s\n", 
		cli.Colorize(cli.ColorBlue, "Source file"),
		cli.Colorize(cli.ColorBold, inputFile))
	fmt.Printf("  %s: %s\n", 
		cli.Colorize(cli.ColorBlue, "File size"), 
		cli.Colorize(cli.ColorBold, fmt.Sprintf("%d bytes", fileStat.Size())))

	// Step 2: Set up replay for each pod
	fmt.Println(cli.Step(2, "Setting up packet replay for pods"))
	
	// Create a WaitGroup to wait for all replays to complete
	var wg sync.WaitGroup
	results := make([]ReplayResult, 0, len(resourceInfo.PodNames))
	resultsMutex := &sync.Mutex{}
	
	// Process each pod
	for _, podName := range resourceInfo.PodNames {
		// Determine the container name for this pod if not specified
		targetContainer := containerName
		if targetContainer == "" {
			// Try to get the default container for this pod
			defaultContainer, err := client.GetDefaultContainer(namespace, podName)
			if err != nil {
				fmt.Printf("  Warning: Failed to get default container for pod %s: %v\n", podName, err)
				// Just use an empty string - the replay will try to pick a valid container
			} else {
				targetContainer = defaultContainer
				fmt.Printf("  Using default container for pod %s: %s\n", podName, targetContainer)
			}
		}
		
		// Display replay setup
		fmt.Printf("  Setting up replay for pod %s\n", cli.Colorize(cli.ColorBold, podName))
		fmt.Printf("    Container: %s\n", targetContainer)
		
		// Add to wait group for concurrent execution
		wg.Add(1)
		
		// Launch replay in a goroutine
		go func(podName, containerName string) {
			defer wg.Done()
			
			// Log that we're starting work on this pod
			fmt.Printf("  → Starting replay on pod: %s\n", cli.Colorize(cli.ColorBold, podName))
			
			// Set up the result struct
			result := ReplayResult{
				PodName:       podName,
				ContainerName: containerName,
			}
			
			// Create a separate client for this replay to avoid race conditions
			podClient, err := k8s.NewClient(client.ConfigPath)
			if err != nil {
				result.Success = false
				result.Error = fmt.Errorf("failed to create k8s client: %v", err)
				resultsMutex.Lock()
				results = append(results, result)
				resultsMutex.Unlock()
				return
			}
			
			// Capture stdout to parse output
			originalStdout := os.Stdout
			r, w, _ := os.Pipe()
			os.Stdout = w
			
			// Execute the replay for this pod
			err = ReplayPackets(podClient, namespace, podName, containerName, inputFile, opts)
			
			// Close writer and restore stdout
			w.Close()
			os.Stdout = originalStdout
			
			// Read captured output
			capturedOutput := make([]byte, 100000)
			n, _ := r.Read(capturedOutput)
			output := string(capturedOutput[:n])
			
			// Try to extract statistics
			packets, bytes := extractReplayStats(output)
			result.PacketsReplayed = packets
			result.BytesReplayed = bytes
			
			if err != nil {
				result.Success = false
				result.Error = err
				fmt.Printf("  ✗ Error replaying to pod %s: %v\n", cli.Colorize(cli.ColorBold, podName), err)
			} else {
				result.Success = true
				fmt.Printf("  ✓ Successfully replayed to pod %s\n", cli.Colorize(cli.ColorBold, podName))
			}
			
			// Add result to the list
			resultsMutex.Lock()
			results = append(results, result)
			resultsMutex.Unlock()
		}(podName, targetContainer)
	}
	
	// Step 3: Wait for all replays to complete
	fmt.Println(cli.Step(3, "Replaying packets to all pods"))
	
	// Wait for all replays to complete
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	
	// Show a spinner while waiting for replays to complete
	ticker := time.NewTicker(200 * time.Millisecond)
	iteration := 0
	select {
	case <-done:
		ticker.Stop()
		fmt.Print("\r  All replays completed!                                 \n")
	case <-time.After(1 * time.Second):
		// Show a spinner only if replays take more than a second
		for {
			select {
			case <-ticker.C:
				fmt.Printf("\r  Waiting for replays to complete %s ", 
					cli.Colorize(cli.ColorBlue, cli.LoadingSpinner(iteration)))
				iteration++
			case <-done:
				ticker.Stop()
				fmt.Print("\r  All replays completed!                                 \n")
				goto exitLoop
			}
		}
	exitLoop:
	}
	
	// Step 4: Show results
	fmt.Println(cli.Step(4, "Results summary"))
	
	// Count successful and failed pods
	successCount := 0
	failedCount := 0
	for _, result := range results {
		if result.Success {
			successCount++
		} else {
			failedCount++
		}
	}
	
	fmt.Printf("  %s: %s\n", 
		cli.Colorize(cli.ColorBlue, "Total Pods"), 
		cli.Colorize(cli.ColorBold, fmt.Sprintf("%d", len(results))))
	fmt.Printf("  %s: %s\n", 
		cli.Colorize(cli.ColorBlue, "Successful"), 
		cli.Colorize(cli.ColorBold, fmt.Sprintf("%d", successCount)))
	fmt.Printf("  %s: %s\n", 
		cli.Colorize(cli.ColorBlue, "Failed"), 
		cli.Colorize(cli.ColorBold, fmt.Sprintf("%d", failedCount)))
	
	// Calculate replay statistics
	totalPackets := 0
	totalBytes := 0
	
	for _, result := range results {
		if result.Success {
			successCount++
			totalPackets += result.PacketsReplayed
			totalBytes += result.BytesReplayed
		} else {
			failedCount++
		}
	}
	
	// Display overall statistics
	fmt.Printf("  %s: %s\n", 
		cli.Colorize(cli.ColorBlue, "Total replays"), 
		cli.Colorize(cli.ColorBold, fmt.Sprintf("%d", len(results))))
	fmt.Printf("  %s: %s\n", 
		cli.Colorize(cli.ColorBlue, "Successful replays"), 
		cli.Colorize(cli.ColorBold, fmt.Sprintf("%d", successCount)))
	if failedCount > 0 {
		fmt.Printf("  %s: %s\n", 
			cli.Colorize(cli.ColorRed, "Failed replays"), 
			cli.Colorize(cli.ColorBold, fmt.Sprintf("%d", failedCount)))
	}
	if totalPackets > 0 {
		fmt.Printf("  %s: %s\n", 
			cli.Colorize(cli.ColorBlue, "Total packets replayed"), 
			cli.Colorize(cli.ColorBold, fmt.Sprintf("%d", totalPackets)))
	}
	if totalBytes > 0 {
		fmt.Printf("  %s: %s\n", 
			cli.Colorize(cli.ColorBlue, "Total bytes replayed"), 
			cli.Colorize(cli.ColorBold, formatBytes(totalBytes)))
	}
	
	// Display individual results
	fmt.Println("  Replay details:")
	for i, result := range results {
		if result.Success {
			fmt.Printf("    %d. %s: %s", 
				i+1,
				cli.Colorize(cli.ColorBold, result.PodName),
				cli.Success("Success"))
			
			if result.PacketsReplayed > 0 {
				fmt.Printf(" (%d packets, %s)", 
					result.PacketsReplayed, 
					formatBytes(result.BytesReplayed))
			}
			fmt.Println()
		} else {
			fmt.Printf("    %d. %s: %s - %v\n", 
				i+1,
				cli.Colorize(cli.ColorBold, result.PodName),
				cli.Colorize(cli.ColorRed, "Failed"),
				result.Error)
		}
	}

	// Final message
	fmt.Printf("\nResource replay completed: %d of %d pods replayed successfully\n", 
		successCount, len(resourceInfo.PodNames))
	
	if successCount == 0 {
		return fmt.Errorf("all replays failed")
	}
	
	return nil
}

// extractReplayStats tries to extract packet and byte counts from tcpreplay output
func extractReplayStats(output string) (int, int) {
	packets := 0
	bytes := 0
	
	// Look for lines that contain packet and byte statistics
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		if strings.Contains(line, "packets") {
			// Try to extract packet count
			var p int
			_, err := fmt.Sscanf(line, "%d packets", &p)
			if err == nil && p > 0 {
				packets = p
			}
		}
		if strings.Contains(line, "bytes") {
			// Try to extract byte count
			var b int
			_, err := fmt.Sscanf(line, "%d bytes", &b)
			if err == nil && b > 0 {
				bytes = b
			}
		}
	}
	
	return packets, bytes
}

// formatBytes formats a byte count in a human-readable form
func formatBytes(bytes int) string {
	if bytes < 1024 {
		return fmt.Sprintf("%d bytes", bytes)
	} else if bytes < 1024*1024 {
		return fmt.Sprintf("%.2f KB", float64(bytes)/1024)
	} else if bytes < 1024*1024*1024 {
		return fmt.Sprintf("%.2f MB", float64(bytes)/(1024*1024))
	} else {
		return fmt.Sprintf("%.2f GB", float64(bytes)/(1024*1024*1024))
	}
}
