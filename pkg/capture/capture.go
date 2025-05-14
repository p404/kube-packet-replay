package capture

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"sync"
	"time"

	"github.com/p404/kube-packet-replay/pkg/cli"
	"github.com/p404/kube-packet-replay/pkg/k8s"
)

// DebugImage is the container image used for network debugging
const DebugImage = "nicolaka/netshoot:latest"

// CapturePackets captures network packets from a Kubernetes pod
func CapturePackets(client *k8s.Client, namespace, podName, containerName, filterExpr, outputFile string, duration time.Duration, verbose bool) error {
	var err error
	
	// Show starting message with date
	currentTime := time.Now().Format("2006-01-02 15:04:05")
	fmt.Printf("\n%s %s\n\n", 
		cli.Colorize(cli.ColorBold, "KUBE-PACKET-REPLAY CAPTURE STARTED AT:"), 
		cli.Colorize(cli.ColorBlue, currentTime))
	
	// Step 1: Setup and validation
	fmt.Println(cli.Step(1, "Setting up packet capture"))
	
	// Use different formatting to emphasize the filter more
	fmt.Printf("  %s\n", cli.Colorize(cli.ColorBold+cli.ColorYellow, "FILTER: '"+filterExpr+"'"))
	
	// Display the target pod name with highlighted formatting to make it clear
	fmt.Printf("  %s: %s\n", 
		cli.Colorize(cli.ColorBlue, "Target Pod"),
		cli.Colorize(cli.ColorBold, podName))
	
	// Display container name if specified
	if containerName != "" {
		fmt.Printf("  %s: %s\n", 
			cli.Colorize(cli.ColorBlue, "Container"),
			cli.Colorize(cli.ColorBold, containerName))
	}
	fmt.Printf("  %s: %s\n", 
		cli.Colorize(cli.ColorBlue, "Namespace"), 
		cli.Colorize(cli.ColorBold, namespace))

	// Create a debug container name with timestamp to avoid collisions
	timestamp := time.Now().Unix()
	debugContainerName := fmt.Sprintf("debug-%s-%d", containerName, timestamp)
	if containerName == "" {
		debugContainerName = fmt.Sprintf("debug-%d", timestamp)
	}

	// Also use timestamp for the output filename if one wasn't specified
	if outputFile == "" || outputFile == fmt.Sprintf("%s.pcap", podName) {
		outputFile = fmt.Sprintf("%s-%d.pcap.gz", podName, timestamp)
	} else if !strings.HasSuffix(outputFile, ".gz") {
		// Add .gz extension if not already present
		outputFile = outputFile + ".gz"
	}

	// Store the original container name for fallback file access
	origContainerName := containerName

	// Construct tcpdump command with shell templates
	// This lets us retrieve the capture file even if tcpdump exits
	interfaceFlag := "any" // Capture on all interfaces

	// Create filenames with timestamp for uniqueness
	rawCaptureFile := fmt.Sprintf("/tmp/capture-%d.pcap", timestamp)
	captureFilename := fmt.Sprintf("/tmp/capture-%d.pcap.gz", timestamp)

	// Build the full tcpdump command using our shell templates
	tcpdumpCmd := BuildTcpdumpCommand(interfaceFlag, filterExpr, rawCaptureFile, captureFilename)

	command := []string{
		"sh", "-c",
		tcpdumpCmd,
	}

	// Create debug container
	if verbose {
		fmt.Println(cli.Info("Debug container command:"))
		fmt.Println(tcpdumpCmd)
	}
	
	fmt.Printf("  Creating debug container %s...\n", cli.Colorize(cli.ColorBold, debugContainerName))
	err = client.CreateDebugContainerWithKubectl(namespace, podName, containerName, DebugImage, command, debugContainerName)
	if err != nil {
		return fmt.Errorf("failed to create debug container: %v", err)
	}
	fmt.Println("  " + cli.Success("Done"))
	fmt.Println()
	
	// Step 2: Capturing packets
	fmt.Println(cli.Step(2, "Capturing network packets"))
	if duration > 0 {
		fmt.Printf("  Capture will run for %s...\n", cli.Colorize(cli.ColorBold, duration.String()))
	} else {
		fmt.Printf("  %s to stop the capture\n", cli.Colorize(cli.ColorYellow, "Press Ctrl+C"))
	}
	
	// Show a spinner while capturing
	spinnerDone := make(chan bool)
	var spinnerClosed sync.Once
	
	// Function to safely close the spinner channel once
	safeCloseSpinner := func() {
		spinnerClosed.Do(func() {
			close(spinnerDone)
		})
	}
	
	go func() {
		iteration := 0
		for {
			select {
			case <-spinnerDone:
				fmt.Print("\r  Capture complete!                      \n")
				return
			default:
				fmt.Printf("\r  Capturing packets %s ", cli.Colorize(cli.ColorBlue, cli.LoadingSpinner(iteration)))
				iteration++
				time.Sleep(200 * time.Millisecond)
			}
		}
	}()

	// Set up signal handling for graceful termination
	ctx, cancel := context.WithCancel(context.Background())
	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, os.Interrupt, syscall.SIGTERM)
	
	// Handle manual interruption with Ctrl+C
	go func() {
		<-signalChan
		fmt.Println(cli.Warning("Interrupt received, stopping capture..."))
		cancel()
		safeCloseSpinner()
	}()

	// Handle duration-based interruption
	if duration > 0 {
		go func() {
			time.Sleep(duration)
			fmt.Println(cli.Info("Duration limit reached, stopping capture..."))
			cancel()
			safeCloseSpinner()
		}()
	}

	// Wait for interruption
	<-ctx.Done()
	
	// Stop the spinner
	safeCloseSpinner()
	time.Sleep(200 * time.Millisecond) // Give spinner a moment to clean up

	fmt.Println("  " + cli.Success("Done"))
	fmt.Println()
	
	// Step 3: Processing capture file
	fmt.Println(cli.Step(3, "Processing capture file"))

	// Get list of containers for better debug container detection
	containers := extractContainersFromPod(client, namespace, podName)
	if len(containers) > 0 && verbose {
		fmt.Printf("  Available containers in pod: %v\n", containers)
	}

	debugContainer := debugContainerName

	if verbose {
		fmt.Printf("  Using debug container: %s\n", debugContainer)
	}

	// Process the capture - First stop tcpdump
	fmt.Printf("  Stopping packet capture...\n")
	tryKillTcpdump(client, namespace, podName, debugContainer, verbose)

	// Wait for compression to complete and retrieve file
	fmt.Printf("  Waiting for container to process capture file...\n")
	output, fileSuccess, _, _ := WaitForCompressedFile(
		client, namespace, podName, debugContainer, 
		rawCaptureFile, captureFilename, verbose)
	
	// If we couldn't get the file from the debug container, try other containers
	if !fileSuccess {
		// Try with the original container
		if origContainerName != "" {
			if verbose {
				fmt.Printf("  Attempting to copy the capture file from the original container: %s\n", origContainerName)
			} else {
				fmt.Printf("  Trying alternative container...\n")
			}

			// Try compressed file first, then raw if needed
			var err2 error
			var localOutput string
			localOutput, err2 = tryExecCat(client, namespace, podName, origContainerName, captureFilename, verbose)
			if err2 != nil && verbose {
				fmt.Printf("  Compressed file not found in original container, trying raw capture file...\n")
				localOutput, err2 = tryExecCat(client, namespace, podName, origContainerName, rawCaptureFile, verbose)
			}
			
			if err2 == nil {
				fileSuccess = true
				output = localOutput
				if verbose {
					fmt.Printf("  Successfully retrieved capture data (%d bytes) from original container.\n", len(output))
				}
			}
		}

		// If still not successful, try other containers
		if !fileSuccess {
			// Try with the debug container again (may still be running)
			if verbose {
				fmt.Printf("  Trying debug container again...\n")
			} else {
				fmt.Printf("  Checking other containers...\n")
			}
			
			var err2 error
			var localOutput string
			localOutput, err2 = tryExecCat(client, namespace, podName, debugContainer, captureFilename, verbose)
			if err2 == nil {
				fileSuccess = true
				output = localOutput
				if verbose {
					fmt.Printf("  Successfully retrieved capture data (%d bytes) from debug container (after stopping tcpdump).\n", len(output))
				}
			}

			// Try each container
			if !fileSuccess {
				// First try each app container in the pod
				for _, c := range containers {
					// Skip debug containers as we already tried those
					if !strings.Contains(c, "debug") {
						if verbose {
							fmt.Printf("  Trying to copy capture file from app container: %s\n", c)
						}
						var err2 error
						var localOutput string
						localOutput, err2 = tryExecCat(client, namespace, podName, c, captureFilename, verbose)
						if err2 == nil {
							if verbose {
								fmt.Printf("  Successfully copied file from container: %s\n", c)
							}
							fileSuccess = true
							output = localOutput
							break
						}
					}
				}

				// If still no success, try debug containers
				if !fileSuccess {
					for _, c := range containers {
						if strings.Contains(c, "debug") && c != debugContainer {
							if verbose {
								fmt.Printf("  Trying to copy capture file from debug container: %s\n", c)
							}
							var err2 error
							var localOutput string
							localOutput, err2 = tryExecCat(client, namespace, podName, c, captureFilename, verbose)
							if err2 == nil {
								if verbose {
									fmt.Printf("  Successfully copied file from container: %s\n", c)
								}
								fileSuccess = true
								output = localOutput
								break
							}
						}
					}
				}
			}
		}
	}

	if !fileSuccess {
		return fmt.Errorf("failed to retrieve capture file from any container")
	}

	fmt.Println("  " + cli.Success("Done"))
	fmt.Println()
	
	// Step 4: Save the file locally
	fmt.Println(cli.Step(4, "Saving capture file"))
	bytes := len(output)
	if verbose {
		fmt.Printf("  Retrieved %d bytes of capture data\n", bytes)
	}
	fmt.Printf("  Saving to %s...\n", outputFile)
	
	var err3 error
	err3 = SaveCaptureToFile(outputFile, output, verbose)
	if err3 != nil {
		return err3
	}
	
	// Show the capture information and then the final done message
	fmt.Printf("  Packet capture downloaded: %s (%s)\n", 
		cli.Colorize(cli.ColorBold, outputFile),
		cli.Colorize(cli.ColorBlue, fmt.Sprintf("%d bytes", bytes)))
	fmt.Println("  " + cli.Success("Done"))
	return nil
}
