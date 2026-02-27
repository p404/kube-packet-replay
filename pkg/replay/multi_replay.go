package replay

import (
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/p404/kube-packet-replay/pkg/k8s"
	outpkg "github.com/p404/kube-packet-replay/pkg/output"
)

// ReplayResult represents the result of replaying packets to a single pod
type ReplayResult struct {
	Error           error
	PodName         string
	ContainerName   string
	PacketsReplayed int
	BytesReplayed   int
	Success         bool
}

// ReplayPacketsToResource replays network packets to all pods that belong to a resource
// (Deployment, StatefulSet, DaemonSet, or individual Pod)
func ReplayPacketsToResource(client *k8s.Client, namespace, resourceName, containerName,
	inputFile string, opts *ReplayOptions) error {

	// Show starting message with date
	out := outpkg.Default()
	currentTime := time.Now().Format("2006-01-02 15:04:05")
	out.Print("\n%s %s\n\n",
		outpkg.FormatBold("KUBE-PACKET-REPLAY RESOURCE REPLAY STARTED AT:"),
		outpkg.Colorize(outpkg.ColorBlue, currentTime))

	// Step 1: Discover resource and pods
	out.Step(1, "Discovering Kubernetes resources")

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
	out.Print("  %s: %s\n",
		outpkg.Colorize(outpkg.ColorBlue, "Resource Type"),
		outpkg.FormatBold(string(resourceInfo.Type)))
	out.Print("  %s: %s\n",
		outpkg.Colorize(outpkg.ColorBlue, "Resource Name"),
		outpkg.FormatBold(resourceInfo.Name))
	out.Print("  %s: %s\n",
		outpkg.Colorize(outpkg.ColorBlue, "Namespace"),
		outpkg.FormatBold(namespace))
	out.Print("  %s: %s\n",
		outpkg.Colorize(outpkg.ColorBlue, "Pods Found"),
		outpkg.FormatBold(fmt.Sprintf("%d", len(resourceInfo.PodNames))))

	// List the pods
	out.Print("  %s:\n", outpkg.Colorize(outpkg.ColorBlue, "Target Pods"))
	for i, podName := range resourceInfo.PodNames {
		out.Print("    %d. %s\n", i+1, outpkg.FormatBold(podName))
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
	out.Print("  %s: %s\n",
		outpkg.Colorize(outpkg.ColorBlue, "Source file"),
		outpkg.FormatBold(inputFile))
	out.Print("  %s: %s\n",
		outpkg.Colorize(outpkg.ColorBlue, "File size"),
		outpkg.FormatBold(fmt.Sprintf("%d bytes", fileStat.Size())))

	// Step 2: Set up replay for each pod
	out.Step(2, "Setting up packet replay for pods")

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
				out.Print("  Warning: Failed to get default container for pod %s: %v\n", podName, err)
				// Just use an empty string - the replay will try to pick a valid container
			} else {
				targetContainer = defaultContainer
				out.Print("  Using default container for pod %s: %s\n", podName, targetContainer)
			}
		}

		// Display replay setup
		out.Print("  Setting up replay for pod %s\n", outpkg.FormatBold(podName))
		out.Print("    Container: %s\n", targetContainer)

		// Add to wait group for concurrent execution
		wg.Add(1)

		// Launch replay in a goroutine
		go func(podName, containerName string) {
			defer wg.Done()

			// Log that we're starting work on this pod
			out.Print("  → Starting replay on pod: %s\n", outpkg.FormatBold(podName))

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

			// Execute the replay for this pod
			err = ReplayPackets(podClient, namespace, podName, containerName, inputFile, opts)

			if err != nil {
				result.Success = false
				result.Error = err
				out.Print("  ✗ Error replaying to pod %s: %v\n", outpkg.FormatBold(podName), err)
			} else {
				result.Success = true
				out.Print("  ✓ Successfully replayed to pod %s\n", outpkg.FormatBold(podName))
			}

			// Add result to the list
			resultsMutex.Lock()
			results = append(results, result)
			resultsMutex.Unlock()
		}(podName, targetContainer)
	}

	// Step 3: Wait for all replays to complete
	out.Step(3, "Replaying packets to all pods")

	// Wait for all replays to complete
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	// Show a spinner while waiting for replays to complete
	spinChars := []string{"|", "/", "-", "\\"}
	ticker := time.NewTicker(200 * time.Millisecond)
	iteration := 0
	select {
	case <-done:
		ticker.Stop()
		out.Print("\r  All replays completed!                                 \n")
	case <-time.After(1 * time.Second):
		// Show a spinner only if replays take more than a second
		for {
			select {
			case <-ticker.C:
				out.Print("\r  Waiting for replays to complete %s ",
					outpkg.Colorize(outpkg.ColorBlue, spinChars[iteration%len(spinChars)]))
				iteration++
			case <-done:
				ticker.Stop()
				out.Print("\r  All replays completed!                                 \n")
				goto exitLoop
			}
		}
	exitLoop:
	}

	// Step 4: Show results
	out.Step(4, "Results summary")

	// Calculate replay statistics
	successCount := 0
	failedCount := 0
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
	out.Print("  %s: %s\n",
		outpkg.Colorize(outpkg.ColorBlue, "Total replays"),
		outpkg.FormatBold(fmt.Sprintf("%d", len(results))))
	out.Print("  %s: %s\n",
		outpkg.Colorize(outpkg.ColorBlue, "Successful"),
		outpkg.FormatBold(fmt.Sprintf("%d", successCount)))
	if failedCount > 0 {
		out.Print("  %s: %s\n",
			outpkg.Colorize(outpkg.ColorRed, "Failed"),
			outpkg.FormatBold(fmt.Sprintf("%d", failedCount)))
	}
	if totalPackets > 0 {
		out.Print("  %s: %s\n",
			outpkg.Colorize(outpkg.ColorBlue, "Total packets replayed"),
			outpkg.FormatBold(fmt.Sprintf("%d", totalPackets)))
	}
	if totalBytes > 0 {
		out.Print("  %s: %s\n",
			outpkg.Colorize(outpkg.ColorBlue, "Total bytes replayed"),
			outpkg.FormatBold(formatBytes(totalBytes)))
	}

	// Display individual results
	out.Println("  Replay details:")
	for i, result := range results {
		if result.Success {
			out.Print("    %d. %s: %s",
				i+1,
				outpkg.FormatBold(result.PodName),
				outpkg.Colorize(outpkg.ColorGreen, "Success"))

			if result.PacketsReplayed > 0 {
				out.Print(" (%d packets, %s)",
					result.PacketsReplayed,
					formatBytes(result.BytesReplayed))
			}
			out.Println()
		} else {
			out.Print("    %d. %s: %s - %v\n",
				i+1,
				outpkg.FormatBold(result.PodName),
				outpkg.Colorize(outpkg.ColorRed, "Failed"),
				result.Error)
		}
	}

	// Final message
	out.Print("\nResource replay completed: %d of %d pods replayed successfully\n",
		successCount, len(resourceInfo.PodNames))

	if successCount == 0 {
		return fmt.Errorf("all replays failed")
	}

	return nil
}

// formatBytes formats a byte count in a human-readable form
func formatBytes(bytes int) string {
	switch {
	case bytes < 1024:
		return fmt.Sprintf("%d bytes", bytes)
	case bytes < 1024*1024:
		return fmt.Sprintf("%.2f KB", float64(bytes)/1024)
	case bytes < 1024*1024*1024:
		return fmt.Sprintf("%.2f MB", float64(bytes)/(1024*1024))
	default:
		return fmt.Sprintf("%.2f GB", float64(bytes)/(1024*1024*1024))
	}
}
