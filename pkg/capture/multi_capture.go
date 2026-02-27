package capture

import (
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/p404/kube-packet-replay/pkg/k8s"
	outpkg "github.com/p404/kube-packet-replay/pkg/output"
)

// MultiCaptureResult represents the result of capturing packets from a single pod
type MultiCaptureResult struct {
	Error           error
	PodName         string
	ContainerName   string
	OutputFile      string
	CapturedBytes   int
	CompressedBytes int
	Success         bool
}

// CapturePacketsFromResource captures network packets from all pods that belong to a resource
// (Deployment, StatefulSet, DaemonSet, or individual Pod)
func CapturePacketsFromResource(client *k8s.Client, namespace, resourceName, containerName,
	filterExpr, outputFileTemplate string, duration time.Duration, verbose bool) error {

	// Show starting message with date
	out := outpkg.Default()
	currentTime := time.Now().Format("2006-01-02 15:04:05")
	out.Print("\n%s %s\n\n",
		out.FormatBold("KUBE-PACKET-REPLAY MULTI-POD CAPTURE STARTED AT:"),
		out.Colorize(outpkg.ColorBlue, currentTime))

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

	// Show resource information
	out.Print("  %s: %s\n",
		out.Colorize(outpkg.ColorBlue, "Resource Type"),
		out.FormatBold(string(resourceInfo.Type)))
	out.Print("  %s: %s\n",
		out.Colorize(outpkg.ColorBlue, "Resource Name"),
		out.FormatBold(resourceInfo.Name))
	out.Print("  %s: %s\n",
		out.Colorize(outpkg.ColorBlue, "Namespace"),
		out.FormatBold(namespace))
	out.Print("  %s: %s\n",
		out.Colorize(outpkg.ColorBlue, "Pods Found"),
		out.FormatBold(fmt.Sprintf("%d", len(resourceInfo.PodNames))))

	// List the pods
	out.Print("  %s:\n", out.Colorize(outpkg.ColorBlue, "Target Pods"))
	for i, podName := range resourceInfo.PodNames {
		out.Print("    %d. %s\n", i+1, out.FormatBold(podName))
	}

	// Use different formatting to emphasize the filter more
	out.Print("  %s\n", out.FormatHighlight("FILTER: '"+filterExpr+"'"))

	// Display capture duration if specified
	if duration > 0 {
		out.Print("  %s: %s\n",
			out.Colorize(outpkg.ColorBlue, "Capture Duration"),
			out.FormatBold(duration.String()))
	}

	// Step 2: Set up capture for each pod
	out.Step(2, "Setting up packet capture for pods")
	out.Println()

	// Create mutexes and channels for synchronization
	results := make([]MultiCaptureResult, 0, len(resourceInfo.PodNames))
	resultsMutex := &sync.Mutex{}
	outputMutex := &sync.Mutex{}

	// Create channels to track capture progress.
	// Use a buffered channel sized to the number of pods to prevent goroutine sends
	// from blocking or panicking after the channel receiver exits.
	captureDone := make(chan struct{}, len(resourceInfo.PodNames))

	// Create a waitgroup for goroutines
	var wg sync.WaitGroup

	// Step 2.1: First setup all pod captures
	type CaptureSetup struct {
		PodName       string
		ContainerName string
		OutputFile    string
	}

	// Use a single timestamp for all captures in this operation
	timestamp := time.Now().Unix()

	// Build a list of all pod capture setups first
	captureSetups := make([]CaptureSetup, 0, len(resourceInfo.PodNames))

	// Setup for each pod
	for _, podName := range resourceInfo.PodNames {
		// Determine the container name for this pod if not specified
		targetContainer := containerName
		if targetContainer == "" {
			// Try to get the default container for this pod
			defaultContainer, err := client.GetDefaultContainer(namespace, podName)
			if err != nil {
				if verbose {
					out.Print("  Warning: Failed to get default container for pod %s: %v\n", podName, err)
				}
				// Just use an empty string - the capture will try to pick a valid container
			} else {
				targetContainer = defaultContainer
				if verbose {
					out.Print("  Using default container for pod %s: %s\n", podName, targetContainer)
				}
			}
		}

		// Create output filename for this pod using the shared timestamp
		outputFile := outputFileTemplate
		switch {
		case outputFile == "":
			outputFile = fmt.Sprintf("%s-%d.pcap.gz", podName, timestamp)
		case strings.Contains(outputFile, "{pod}"):
			// Replace {pod} with the actual pod name
			outputFile = strings.ReplaceAll(outputFile, "{pod}", podName)
		case strings.Contains(outputFile, "{resource}"):
			// Replace {resource} with the resource name
			outputFile = strings.ReplaceAll(outputFile, "{resource}", resourceInfo.Name)
		case len(resourceInfo.PodNames) > 1:
			// If capturing from multiple pods, make sure filenames are unique
			// Only transform the filename if it doesn't already have pod/resource placeholders
			ext := filepath.Ext(outputFile)
			base := strings.TrimSuffix(outputFile, ext)
			outputFile = fmt.Sprintf("%s-%s%s", base, podName, ext)
		}

		// Add .gz extension if not already present
		if !strings.HasSuffix(outputFile, ".gz") {
			outputFile += ".gz"
		}

		// Display capture setup info
		out.Print("  Setting up capture for pod %s\n", out.FormatBold(podName))
		out.Print("    Container: %s\n", targetContainer)
		out.Print("    Output file: %s\n", outputFile)

		// Add to list of setups
		captureSetups = append(captureSetups, CaptureSetup{
			PodName:       podName,
			ContainerName: targetContainer,
			OutputFile:    outputFile,
		})
	}

	// Step 2.2: Now launch captures for all pods after setup is complete
	fmt.Println()
	out.Info("  Launching captures for all pods...")
	time.Sleep(500 * time.Millisecond) // Small delay for visual clarity

	// Launch all captures one by one
	for i := range captureSetups {
		// The pod being captured
		podName := captureSetups[i].PodName
		containerName := captureSetups[i].ContainerName
		outputFile := captureSetups[i].OutputFile

		// Add this pod to the wait group
		wg.Add(1)

		// Launch capture in a goroutine for this pod
		go func(podName, containerName, outputFile string) {

			defer func() {
				wg.Done()
				// Notify main routine that this capture is complete
				captureDone <- struct{}{}
			}()

			// Use mutex to synchronize console output
			outputMutex.Lock()
			out.Print("  → Starting capture on pod: %s\n", out.FormatBold(podName))
			outputMutex.Unlock()

			// Set up the result struct
			result := MultiCaptureResult{
				PodName:       podName,
				ContainerName: containerName,
				OutputFile:    outputFile,
			}

			// Create a separate client for this capture to avoid race conditions
			podClient, err := k8s.NewClient(client.ConfigPath)
			if err != nil {
				result.Success = false
				result.Error = fmt.Errorf("failed to create k8s client: %v", err)
				resultsMutex.Lock()
				results = append(results, result)
				resultsMutex.Unlock()
				return
			}

			// Execute the capture for this pod
			err = captureSinglePod(podClient, namespace, podName, containerName,
				filterExpr, outputFile, duration, verbose)

			if err != nil {
				result.Success = false
				result.Error = err

				// Synchronize error message output
				outputMutex.Lock()
				out.Print("  ✗ Error capturing from pod %s: %v\n", out.FormatBold(podName), err)
				outputMutex.Unlock()
			} else {
				result.Success = true

				// Use the outputMutex to ensure clean output
				outputMutex.Lock()

				// Clear any existing spinner output with a newline and blank line
				fmt.Print("\n") // Move to a new line from where the spinner might be

				// Start the saving capture output
				out.Print("→ Saving capture file\n")
				out.Print("  Saving to %s...\n", outputFile)
				outputMutex.Unlock()

				// Small delay to simulate download progress
				time.Sleep(300 * time.Millisecond)

				// Get file size information first so we can report it
				fileInfo, statErr := os.Stat(outputFile)
				var fileSize int64
				if statErr == nil {
					fileSize = fileInfo.Size()
					result.CapturedBytes = int(fileSize) // Update the result struct with the correct size
				}

				// Show success with file details
				outputMutex.Lock()
				out.Print("  Packet capture downloaded: %s (%d bytes)\n", outputFile, fileSize)
				out.Print("  ✓ Done\n")
				out.Print("  ✓ Successfully captured from pod %s\n", out.FormatBold(podName))
				outputMutex.Unlock()

				// Get file size information
				fileInfo, err := os.Stat(outputFile)
				if err == nil {
					result.CapturedBytes = int(fileInfo.Size())

					// Provide detailed info about capture size with synchronized output
					if verbose {
						// Format bytes in a human-readable way
						size := float64(result.CapturedBytes)
						unit := "B"
						if size > 1024 {
							size /= 1024
							unit = "KB"
						}
						if size > 1024 {
							size /= 1024
							unit = "MB"
						}

						outputMutex.Lock()
						out.Print("    Captured %.2f %s to %s\n", size, unit, outputFile)
						outputMutex.Unlock()
					}
				}
			}

			// Add result to the list
			resultsMutex.Lock()
			results = append(results, result)
			resultsMutex.Unlock()
		}(podName, containerName, outputFile)
	}

	// Step 3: Capturing packets
	out.Step(3, "Capturing network packets")
	out.Println()

	// Set up signal handling for graceful termination
	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(signalChan)

	// Handle manual interruption with Ctrl+C
	go func() {
		<-signalChan
		out.Warning("\nInterrupt received, stopping captures...")
	}()

	// Start capturing packets with a spinner to show progress
	out.Info("  Capturing packets from all pods...")

	// Show capture duration as simple text, will be followed by progress bar
	if duration > 0 {
		// Calculate the end time
		endTime := time.Now().Add(duration)
		endTimeStr := endTime.Format("15:04:05")

		// Simple highlight for progress information
		fmt.Println()
		out.Print("  %s\n", out.FormatHighlight("CAPTURE DURATION: "+duration.String()))
		out.Print("  %s\n", out.Colorize(outpkg.ColorBlue, "End Time: "+endTimeStr))
		fmt.Println()
	}

	// Track active captures
	activeCaptures := len(resourceInfo.PodNames)

	// Setup spinner to show ongoing capture status
	spinnerDone := make(chan bool)
	var spinnerWg sync.WaitGroup
	spinnerWg.Add(1)

	// Start a simple countdown timer if we have a duration
	if duration > 0 {
		go func() {
			defer spinnerWg.Done()

			// Wait for initial messages to be printed
			time.Sleep(3 * time.Second)

			// Print directly to stderr which won't be captured
			logf := func(format string, args ...interface{}) {
				fmt.Fprintf(os.Stderr, format, args...)
			}

			// Announce the countdown start - this will definitely be visible
			logf("\n  CAPTURE PROGRESS (DURATION: %s)\n", duration.String())
			logf("  ----------------------------------------\n")

			// Start a spinner to show capture is in progress
			spinner := out.StartSpinner("Capturing network packets...")
			defer out.StopSpinner(spinner)

			// Use a simple 5-second ticker
			ticker := time.NewTicker(5 * time.Second)
			defer ticker.Stop()

			// Start time for calculations
			startTime := time.Now()
			endTime := startTime.Add(duration)

			// For stopping the progress display
			isCanceled := false
			go func() {
				<-spinnerDone
				isCanceled = true
			}()

			// Last percentage logged (in 10% increments)
			lastStep := -1

			// Main countdown loop - report every 5 seconds
			for !isCanceled {
				select {
				case <-ticker.C:
					// Get current time and calculate progress
					now := time.Now()
					elapsed := now.Sub(startTime)
					remaining := endTime.Sub(now)

					// Guard against negative remaining time
					if remaining < 0 {
						remaining = 0
					}

					// Calculate percentage complete (0-100)
					percentComplete := int((float64(elapsed) / float64(duration)) * 100)
					if percentComplete > 100 {
						percentComplete = 100
					}

					// Round to nearest 10% for logging
					progressStep := (percentComplete / 10) * 10

					// Log the progress message directly to stderr
					elapsedStr := formatDurationHMS(elapsed)
					remainingStr := formatDurationHMS(remaining)

					// Report all progress info, but highlight 10% increments
					if progressStep != lastStep {
						// Print a milestone message for 10% increments
						logf("  ⭐ MILESTONE: %d%% COMPLETE | %s elapsed | %s remaining\n",
							progressStep, elapsedStr, remainingStr)
						lastStep = progressStep
					} else {
						// Regular update between milestones
						logf("  ⏰ Progress: %d%% | %s elapsed | %s remaining\n",
							percentComplete, elapsedStr, remainingStr)
					}
				case <-captureDone:
					activeCaptures--
					if activeCaptures <= 0 {
						out.StopSpinner(spinner)
						logf("\n  ✅ CAPTURE COMPLETED!\n")
						return
					}
				}
			}
		}()
	} else {
		// For non-timed captures, show Ctrl+C message and a spinner
		go func() {
			defer spinnerWg.Done()
			time.Sleep(2 * time.Second)
			out.Print("  %s to stop the capture\n", out.Colorize(outpkg.ColorYellow, "Press Ctrl+C"))

			// Start a spinner to show capture is in progress
			spinner := out.StartSpinner("Capturing network packets...")
			defer out.StopSpinner(spinner)

			// Just wait for completion
			for range captureDone {
				activeCaptures--
				if activeCaptures <= 0 {
					out.StopSpinner(spinner)
					out.Print("  %s All captures completed!\n", out.Colorize(outpkg.ColorGreen, "✓"))
					return
				}
			}
		}()
	}

	// Wait for all captures to complete
	wg.Wait()

	// Make sure spinner is stopped
	if activeCaptures > 0 {
		close(spinnerDone)
	}
	spinnerWg.Wait()
	time.Sleep(200 * time.Millisecond) // Give the terminal a moment to redraw

	// Step 4: Show results
	out.Step(4, "Results summary")

	// Count successful and failed pods
	successCount := 0
	failedCount := 0
	totalBytes := 0
	for _, result := range results {
		if result.Success {
			successCount++
			// Use CapturedBytes instead of CompressedBytes
			totalBytes += result.CapturedBytes
		} else {
			failedCount++
		}
	}

	// Display overall statistics
	out.Print("  %s: %s\n",
		out.Colorize(outpkg.ColorBlue, "Total captures"),
		out.FormatBold(fmt.Sprintf("%d", len(results))))
	out.Print("  %s: %s\n",
		out.Colorize(outpkg.ColorBlue, "Successful captures"),
		out.FormatBold(fmt.Sprintf("%d", successCount)))
	if failedCount > 0 {
		out.Print("  %s: %s\n",
			out.Colorize(outpkg.ColorRed, "Failed captures"),
			out.FormatBold(fmt.Sprintf("%d", failedCount)))
	}
	out.Print("  %s: %s\n",
		out.Colorize(outpkg.ColorBlue, "Total captured data"),
		out.FormatBold(formatBytesMulti(totalBytes)))

	// Display individual results
	out.Println("  Capture details:")
	for i, result := range results {
		if result.Success {
			out.Print("    %d. %s: %s (%s)\n",
				i+1,
				out.FormatBold(result.PodName),
				out.Colorize(outpkg.ColorGreen, "Success"),
				formatBytesMulti(result.CapturedBytes))
		} else {
			out.Print("    %d. %s: %s - %v\n",
				i+1,
				out.FormatBold(result.PodName),
				out.Colorize(outpkg.ColorRed, "Failed"),
				result.Error)
		}
	}

	// Final message
	out.Print("\nResource capture completed: %d of %d pods captured successfully\n",
		successCount, len(resourceInfo.PodNames))

	if successCount == 0 {
		return fmt.Errorf("all captures failed")
	}

	return nil
}

// captureSinglePod is a helper function that captures packets from a single pod.
// It is a wrapper around the original CapturePackets function with error handling.
func captureSinglePod(client *k8s.Client, namespace, podName, containerName,
	filterExpr, outputFile string, duration time.Duration, verbose bool) error {

	// Call the original CapturePackets function directly.
	// Note: We no longer manipulate os.Stdout globally as that causes race conditions
	// in concurrent goroutines. Output suppression should be handled via the Writer interface.
	return CapturePackets(client, namespace, podName, containerName,
		filterExpr, outputFile, duration, verbose)
}

// formatBytesMulti and formatDurationHMS are defined in file_retrieval.go
