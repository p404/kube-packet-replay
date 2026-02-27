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
	outpkg "github.com/p404/kube-packet-replay/pkg/output"
)

// DefaultdebugImage is the container image used for network debugging.
// Pinned to a specific version to avoid supply chain risks.
var debugImage = "nicolaka/netshoot:v0.13"

// SetDebugImage overrides the default debug container image
func SetDebugImage(image string) {
	debugImage = image
}

// CapturePackets captures network packets from a Kubernetes pod
func CapturePackets(client *k8s.Client, namespace, podName, containerName, filterExpr, outputFile string, duration time.Duration, verbose bool) error {
	var err error
	out := outpkg.Default()

	// Show starting message with date
	currentTime := time.Now().Format("2006-01-02 15:04:05")
	out.Print("\n%s %s\n\n",
		out.FormatBold("KUBE-PACKET-REPLAY CAPTURE STARTED AT:"),
		out.Colorize(outpkg.ColorBlue, currentTime))

	// Step 1: Setup and validation
	out.Step(1, "Setting up packet capture")

	// Use different formatting to emphasize the filter more
	out.Print("  %s\n", out.FormatHighlight("FILTER: '"+filterExpr+"'"))

	// Display the target pod name with highlighted formatting to make it clear
	out.Print("  %s: %s\n",
		out.Colorize(outpkg.ColorBlue, "Target Pod"),
		out.FormatBold(podName))

	// Display container name if specified
	if containerName != "" {
		out.Print("  %s: %s\n",
			out.Colorize(outpkg.ColorBlue, "Container"),
			out.FormatBold(containerName))
	}
	out.Print("  %s: %s\n",
		out.Colorize(outpkg.ColorBlue, "Namespace"),
		out.FormatBold(namespace))

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
		outputFile += ".gz"
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
		out.Info("Debug container command:")
		out.Println(tcpdumpCmd)
	}

	out.Print("  Creating debug container %s...\n", out.FormatBold(debugContainerName))
	err = client.CreateDebugContainerWithKubectl(namespace, podName, containerName, debugImage, command, debugContainerName)
	if err != nil {
		return fmt.Errorf("failed to create debug container: %v", err)
	}
	out.Success("  Done")
	out.Println()

	// Step 2: Capturing packets
	out.Step(2, "Capturing network packets")
	if duration > 0 {
		out.Print("  Capture will run for %s...\n", out.FormatBold(duration.String()))
	} else {
		out.Print("  %s to stop the capture\n", out.Colorize(outpkg.ColorYellow, "Press Ctrl+C"))
	}

	// Show a spinner while capturing
	spinner := out.StartSpinner("Capturing packets")

	// Set up signal handling for graceful termination
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(signalChan)

	// Handle manual interruption with Ctrl+C
	go func() {
		<-signalChan
		out.Warning("Interrupt received, stopping capture...")
		cancel()
		out.StopSpinner(spinner)
	}()

	// Handle duration-based interruption
	if duration > 0 {
		go func() {
			time.Sleep(duration)
			out.Info("Duration limit reached, stopping capture...")
			cancel()
			out.StopSpinner(spinner)
		}()
	}

	// Wait for interruption
	<-ctx.Done()

	// Stop the spinner
	out.StopSpinner(spinner)
	time.Sleep(200 * time.Millisecond) // Give spinner a moment to clean up

	out.Success("  Done")
	out.Println()

	// Step 3: Processing capture file
	out.Step(3, "Processing capture file")

	// Get list of containers for better debug container detection
	containers := extractContainersFromPod(client, namespace, podName)
	if len(containers) > 0 && verbose {
		out.Print("  Available containers in pod: %v\n", containers)
	}

	debugContainer := debugContainerName

	if verbose {
		out.Print("  Using debug container: %s\n", debugContainer)
	}

	// Process the capture - First stop tcpdump
	out.Print("  Stopping packet capture...\n")
	_ = tryKillTcpdump(client, namespace, podName, debugContainer, verbose)

	// Wait for compression to complete and retrieve file
	out.Print("  Waiting for container to process capture file...\n")
	captureData, fileSuccess, _, _ := WaitForCompressedFile(
		client, namespace, podName, debugContainer,
		rawCaptureFile, captureFilename, verbose)

	// If we couldn't get the file from the debug container, try other containers
	if !fileSuccess {
		// Try with the original container
		if origContainerName != "" {
			if verbose {
				out.Print("  Attempting to copy the capture file from the original container: %s\n", origContainerName)
			} else {
				out.Print("  Trying alternative container...\n")
			}

			// Try compressed file first, then raw if needed
			var err2 error
			var localOutput string
			localOutput, err2 = tryExecCat(client, namespace, podName, origContainerName, captureFilename, verbose)
			if err2 != nil && verbose {
				out.Print("  Compressed file not found in original container, trying raw capture file...\n")
				localOutput, err2 = tryExecCat(client, namespace, podName, origContainerName, rawCaptureFile, verbose)
			}

			if err2 == nil {
				fileSuccess = true
				captureData = localOutput
				if verbose {
					out.Print("  Successfully retrieved capture data (%d bytes) from original container.\n", len(captureData))
				}
			}
		}

		// If still not successful, try other containers
		if !fileSuccess {
			// Try with the debug container again (may still be running)
			if verbose {
				out.Print("  Trying debug container again...\n")
			} else {
				out.Print("  Checking other containers...\n")
			}

			var err2 error
			var localOutput string
			localOutput, err2 = tryExecCat(client, namespace, podName, debugContainer, captureFilename, verbose)
			if err2 == nil {
				fileSuccess = true
				captureData = localOutput
				if verbose {
					out.Print("  Successfully retrieved capture data (%d bytes) from debug container (after stopping tcpdump).\n", len(captureData))
				}
			}

			// Try each container
			if !fileSuccess {
				// First try each app container in the pod
				for _, c := range containers {
					// Skip debug containers as we already tried those
					if strings.Contains(c, "debug") {
						continue
					}
					if verbose {
						out.Print("  Trying to copy capture file from app container: %s\n", c)
					}
					var err2 error
					var localOutput string
					localOutput, err2 = tryExecCat(client, namespace, podName, c, captureFilename, verbose)
					if err2 == nil {
						if verbose {
							out.Print("  Successfully copied file from container: %s\n", c)
						}
						fileSuccess = true
						captureData = localOutput
						break
					}
				}

				// If still no success, try debug containers
				if !fileSuccess {
					for _, c := range containers {
						if !strings.Contains(c, "debug") || c == debugContainer {
							continue
						}
						if verbose {
							out.Print("  Trying to copy capture file from debug container: %s\n", c)
						}
						var err2 error
						var localOutput string
						localOutput, err2 = tryExecCat(client, namespace, podName, c, captureFilename, verbose)
						if err2 == nil {
							if verbose {
								out.Print("  Successfully copied file from container: %s\n", c)
							}
							fileSuccess = true
							captureData = localOutput
							break
						}
					}
				}
			}
		}
	}

	if !fileSuccess {
		return fmt.Errorf("failed to retrieve capture file from any container")
	}

	out.Success("  Done")
	out.Println()

	// Step 4: Save the file locally
	out.Step(4, "Saving capture file")
	bytes := len(captureData)
	if verbose {
		out.Print("  Retrieved %d bytes of capture data\n", bytes)
	}
	out.Print("  Saving to %s...\n", outputFile)

	err3 := SaveCaptureToFile(outputFile, captureData, verbose)
	if err3 != nil {
		return err3
	}

	// Show the capture information and then the final done message
	out.Print("  Packet capture downloaded: %s (%s)\n",
		out.FormatBold(outputFile),
		out.Colorize(outpkg.ColorGreen, formatBytes(int64(bytes))))
	out.Success("  Done")
	return nil
}
