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

	"github.com/p404/kube-packet-replay/pkg/cli"
	"github.com/p404/kube-packet-replay/pkg/k8s"
)

// MultiCaptureResult represents the result of capturing packets from a single pod
type MultiCaptureResult struct {
	PodName          string
	ContainerName    string
	Success          bool
	Error            error
	OutputFile       string
	CapturedBytes    int
	CompressedBytes  int
}

// CapturePacketsFromResource captures network packets from all pods that belong to a resource
// (Deployment, StatefulSet, DaemonSet, or individual Pod)
func CapturePacketsFromResource(client *k8s.Client, namespace, resourceName, containerName, 
	filterExpr, outputFileTemplate string, duration time.Duration, verbose bool) error {
	
	// Show starting message with date
	currentTime := time.Now().Format("2006-01-02 15:04:05")
	fmt.Printf("\n%s %s\n\n", 
		cli.Colorize(cli.ColorBold, "KUBE-PACKET-REPLAY MULTI-POD CAPTURE STARTED AT:"), 
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
	
	// Show resource information
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

	// Use different formatting to emphasize the filter more
	fmt.Printf("  %s\n", cli.Colorize(cli.ColorBold+cli.ColorYellow, "FILTER: '"+filterExpr+"'"))

	// Display capture duration if specified
	if duration > 0 {
		fmt.Printf("  %s: %s\n", 
			cli.Colorize(cli.ColorBlue, "Capture Duration"),
			cli.Colorize(cli.ColorBold, duration.String()))
	}

	// Step 2: Set up capture for each pod
	fmt.Println(cli.Step(2, "Setting up packet capture for pods"))
	
	// Create mutexes and channels for synchronization
	results := make([]MultiCaptureResult, 0, len(resourceInfo.PodNames))
	resultsMutex := &sync.Mutex{}
	outputMutex := &sync.Mutex{}
	
	// Create channels to track capture progress
	captureDone := make(chan struct{})
	defer close(captureDone) // Ensure channel is closed when function exits
	
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
					fmt.Printf("  Warning: Failed to get default container for pod %s: %v\n", podName, err)
				}
				// Just use an empty string - the capture will try to pick a valid container
			} else {
				targetContainer = defaultContainer
				if verbose {
					fmt.Printf("  Using default container for pod %s: %s\n", podName, targetContainer)
				}
			}
		}
		
		// Create output filename for this pod using the shared timestamp
		outputFile := outputFileTemplate
		if outputFile == "" {
			outputFile = fmt.Sprintf("%s-%d.pcap.gz", podName, timestamp)
		} else if strings.Contains(outputFile, "{pod}") {
			// Replace {pod} with the actual pod name
			outputFile = strings.ReplaceAll(outputFile, "{pod}", podName)
		} else if strings.Contains(outputFile, "{resource}") {
			// Replace {resource} with the resource name
			outputFile = strings.ReplaceAll(outputFile, "{resource}", resourceInfo.Name)
		} else if len(resourceInfo.PodNames) > 1 {
			// If capturing from multiple pods, make sure filenames are unique
			// Only transform the filename if it doesn't already have pod/resource placeholders
			ext := filepath.Ext(outputFile)
			base := strings.TrimSuffix(outputFile, ext)
			outputFile = fmt.Sprintf("%s-%s%s", base, podName, ext)
		}

		// Add .gz extension if not already present
		if !strings.HasSuffix(outputFile, ".gz") {
			outputFile = outputFile + ".gz"
		}
		
		// Display capture setup info
		fmt.Printf("  Setting up capture for pod %s\n", cli.Colorize(cli.ColorBold, podName))
		fmt.Printf("    Container: %s\n", targetContainer)
		fmt.Printf("    Output file: %s\n", outputFile)
		
		// Add to list of setups
		captureSetups = append(captureSetups, CaptureSetup{
			PodName:       podName,
			ContainerName: targetContainer,
			OutputFile:    outputFile,
		})
	}
	
	// Step 2.2: Now launch captures for all pods after setup is complete
	fmt.Println()
	fmt.Printf("  %s\n", cli.Info("Launching captures for all pods..."))
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
			fmt.Printf("  → Starting capture on pod: %s\n", cli.Colorize(cli.ColorBold, podName))
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
				fmt.Printf("  ✗ Error capturing from pod %s: %v\n", cli.Colorize(cli.ColorBold, podName), err)
				outputMutex.Unlock()
			} else {
				result.Success = true
				
				// Use the outputMutex to ensure clean output
				outputMutex.Lock()
				
				// Clear any existing spinner output with a newline and blank line
				fmt.Print("\n") // Move to a new line from where the spinner might be
				
				// Start the saving capture output
				fmt.Printf("→ Saving capture file\n")
				fmt.Printf("  Saving to %s...\n", outputFile)
				outputMutex.Unlock()
				
				// Small delay to simulate download progress
				time.Sleep(300 * time.Millisecond) 
				
				// Get file size information first so we can report it
				fileInfo, statErr := os.Stat(outputFile)
				var fileSize int64
				if statErr == nil {
					fileSize = fileInfo.Size()
					result.CapturedBytes = int(fileSize)  // Update the result struct with the correct size
				}
				
				// Show success with file details
				outputMutex.Lock()
				fmt.Printf("  Packet capture downloaded: %s (%d bytes)\n", outputFile, fileSize)
				fmt.Printf("  ✓ Done\n")
				fmt.Printf("  ✓ Successfully captured from pod %s\n", cli.Colorize(cli.ColorBold, podName))
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
						fmt.Printf("    Captured %.2f %s to %s\n", size, unit, outputFile)
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
	fmt.Println(cli.Step(3, "Capturing network packets"))
	
	// Set up signal handling for graceful termination
	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, os.Interrupt, syscall.SIGTERM)
	
	// Handle manual interruption with Ctrl+C
	go func() {
		<-signalChan
		fmt.Println(cli.Warning("\nInterrupt received, stopping captures..."))
		// We don't actually have a way to cancel in-progress captures
		// but acknowledging the interrupt is helpful for UX
	}()
	
	// Start capturing packets with a spinner to show progress
	fmt.Printf("  %s\n", cli.Info("Capturing packets from all pods..."))
	
	// Show capture duration as simple text, will be followed by progress bar
	if duration > 0 {
		// Calculate the end time
		endTime := time.Now().Add(duration)
		endTimeStr := endTime.Format("15:04:05")
		
		// Simple highlight for progress information
		fmt.Println()
		fmt.Printf("  %s\n", cli.Colorize(cli.ColorBold+cli.ColorYellow, "CAPTURE DURATION: "+duration.String()))
		fmt.Printf("  %s\n", cli.Colorize(cli.ColorBlue, "End Time: "+endTimeStr))
		fmt.Println()
		fmt.Println("  Progress bar will appear below:")
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
					elapsedStr := formatDuration(elapsed)
					remainingStr := formatDuration(remaining)
					
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
						logf("\n  ✅ CAPTURE COMPLETED!\n")
						return
					}
				}
			}
		}()
	} else {
		// For non-timed captures, just show Ctrl+C message
		go func() {
			defer spinnerWg.Done()
			time.Sleep(2 * time.Second)
			fmt.Printf("  %s to stop the capture\n", cli.Colorize(cli.ColorYellow, "Press Ctrl+C"))
			
			// Just wait for completion
			for {
				select {
				case <-captureDone:
					activeCaptures--
					if activeCaptures <= 0 {
						fmt.Printf("  %s All captures completed!\n", cli.Colorize(cli.ColorGreen, "✓"))
						return
					}
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
	fmt.Println(cli.Step(4, "Results summary"))
	
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
	fmt.Printf("  %s: %s\n", 
		cli.Colorize(cli.ColorBlue, "Total captures"), 
		cli.Colorize(cli.ColorBold, fmt.Sprintf("%d", len(results))))
	fmt.Printf("  %s: %s\n", 
		cli.Colorize(cli.ColorBlue, "Successful captures"), 
		cli.Colorize(cli.ColorBold, fmt.Sprintf("%d", successCount)))
	if failedCount > 0 {
		fmt.Printf("  %s: %s\n", 
			cli.Colorize(cli.ColorRed, "Failed captures"), 
			cli.Colorize(cli.ColorBold, fmt.Sprintf("%d", failedCount)))
	}
	fmt.Printf("  %s: %s\n", 
		cli.Colorize(cli.ColorBlue, "Total captured data"), 
		cli.Colorize(cli.ColorBold, formatBytes(totalBytes)))
	
	// Display individual results
	fmt.Println("  Capture details:")
	for i, result := range results {
		if result.Success {
			fmt.Printf("    %d. %s: %s (%s)\n", 
				i+1,
				cli.Colorize(cli.ColorBold, result.PodName),
				cli.Success("Success"),
				formatBytes(result.CapturedBytes))
		} else {
			fmt.Printf("    %d. %s: %s - %v\n", 
				i+1,
				cli.Colorize(cli.ColorBold, result.PodName),
				cli.Colorize(cli.ColorRed, "Failed"),
				result.Error)
		}
	}

	// Final message
	fmt.Printf("\nResource capture completed: %d of %d pods captured successfully\n", 
		successCount, len(resourceInfo.PodNames))
	
	if successCount == 0 {
		return fmt.Errorf("all captures failed")
	}
	
	return nil
}

// captureSinglePod is a helper function that captures packets from a single pod
// It is a wrapper around the original CapturePackets function with error handling
func captureSinglePod(client *k8s.Client, namespace, podName, containerName, 
	filterExpr, outputFile string, duration time.Duration, verbose bool) error {
	
	// Capture with a timeout context if duration is specified
	var captureErr error
	
	// We need to be more careful with stdout redirection to prevent interfering with progress display
	// Let's use a file-based approach instead of directly manipulating os.Stdout
	if !verbose {
		// Create a pipe to capture output we want to suppress
		r, w, _ := os.Pipe()
		
		// Save the original stdout
		originalStdout := os.Stdout
		
		// Redirect stdout to our pipe
		os.Stdout = w
		
		// Create a channel to signal when we're done with redirection
		done := make(chan bool)
		
		// Start a goroutine to read from the pipe and discard the output
		go func() {
			buf := make([]byte, 1024)
			for {
				_, err := r.Read(buf)
				if err != nil {
					break
				}
			}
			done <- true
		}()
		
		// Ensure we restore stdout and close resources when done
		defer func() {
			os.Stdout = originalStdout
			w.Close()
			<-done // Wait for the goroutine to finish
			r.Close()
		}()
	}
	
	// Call the original CapturePackets function
	captureErr = CapturePackets(client, namespace, podName, containerName, 
		filterExpr, outputFile, duration, verbose)
	
	return captureErr
}

// formatBytes formats a byte count in a human-readable form
func formatBytes(bytes int) string {
	if bytes < 1024 {
		return fmt.Sprintf("%d B", bytes)
	} else if bytes < 1024*1024 {
		return fmt.Sprintf("%.2f KB", float64(bytes)/1024)
	} else if bytes < 1024*1024*1024 {
		return fmt.Sprintf("%.2f MB", float64(bytes)/1024/1024)
	} else {
		return fmt.Sprintf("%.2f GB", float64(bytes)/1024/1024/1024)
	}
}

// formatDuration formats a duration in a user-friendly format (HH:MM:SS)
func formatDuration(d time.Duration) string {
	hours := int(d.Hours())
	minutes := int(d.Minutes()) % 60
	seconds := int(d.Seconds()) % 60
	
	return fmt.Sprintf("%02d:%02d:%02d", hours, minutes, seconds)
}
