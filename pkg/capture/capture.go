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

	// Also use timestamp for the output filename if one wasn't specified
	if outputFile == "" || outputFile == fmt.Sprintf("%s.pcap", podName) {
		outputFile = fmt.Sprintf("%s-%d.pcap.gz", podName, timestamp)
	} else if !strings.HasSuffix(outputFile, ".gz") {
		// Add .gz extension if not already present
		outputFile = outputFile + ".gz"
	}

	fmt.Printf("Creating debug container with name: %s in pod: %s\n", debugContainerName, podName)

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

	// Create debug container using kubectl debug
	fmt.Println("Debug container command:\n")
	fmt.Println(tcpdumpCmd)
	err := client.CreateDebugContainerWithKubectl(namespace, podName, containerName, DebugImage, command, debugContainerName)
	if err != nil {
		return fmt.Errorf("failed to create debug container: %v", err)
	}

	fmt.Printf("Debug container '%s' created. Starting packet capture...\n", debugContainerName)

	// Set up signal handling for graceful termination
	ctx, cancel := context.WithCancel(context.Background())
	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, os.Interrupt, syscall.SIGTERM)
	
	// Handle manual interruption with Ctrl+C
	go func() {
		<-signalChan
		fmt.Println("Interrupt received, stopping capture...")
		cancel()
	}()

	// Handle duration-based interruption
	if duration > 0 {
		go func() {
			fmt.Printf("Capture will run for %v...\n", duration)
			time.Sleep(duration)
			fmt.Println("Duration limit reached, stopping capture...")
			cancel()
		}()
	} else {
		fmt.Println("Press Ctrl+C to stop the capture...")
	}

	// Wait for interruption
	<-ctx.Done()

	// Get list of containers for better debug container detection
	containers := extractContainersFromPod(client, namespace, podName)
	if len(containers) > 0 && verbose {
		fmt.Printf("Available containers in pod: %v\n", containers)
	}

	debugContainer := debugContainerName

	if verbose {
		fmt.Printf("Using debug container: %s\n", debugContainer)
	}

	// Stop tcpdump first to trigger container-side compression
	fmt.Printf("Attempting to stop tcpdump in debug container '%s'...\n", debugContainer)
	tryKillTcpdump(client, namespace, podName, debugContainer, verbose)

	// Wait for compression to complete and retrieve file
	fmt.Println("Waiting for container to compress the capture file...")
	output, fileSuccess, _, _ := WaitForCompressedFile(
		client, namespace, podName, debugContainer, 
		rawCaptureFile, captureFilename, verbose)

	// If we couldn't get the file from the debug container, try other containers
	if !fileSuccess {
		// Copy the capture file from the pod
		fmt.Println("Copying capture file from the pod...")

		// Try with the original container
		if origContainerName != "" {
			if verbose {
				fmt.Printf("Attempting to copy the capture file from the original container: %s\n", origContainerName)
			}

			// Try compressed file first, then raw if needed
			output, err = tryExecCat(client, namespace, podName, origContainerName, captureFilename, verbose)
			if err != nil && verbose {
				fmt.Printf("Compressed file not found in original container, trying raw capture file...\n")
				output, err = tryExecCat(client, namespace, podName, origContainerName, rawCaptureFile, verbose)
			}
			
			if err != nil {
				if verbose {
					fmt.Printf("Failed to cat file from original container: %v\n", err)
				}
			} else {
				fileSuccess = true
				fmt.Printf("Successfully retrieved capture data (%d bytes) from original container.\n", len(output))
			}
		}

		// If still not successful, try other containers
		if !fileSuccess {
			// Try with the debug container again (may still be running)
			output, err = tryExecCat(client, namespace, podName, debugContainer, captureFilename, verbose)
			if err != nil {
				if verbose {
					fmt.Printf("Failed to cat file from debug container after stopping tcpdump: %v\n", err)
				}
			} else {
				fileSuccess = true
				fmt.Printf("Successfully retrieved capture data (%d bytes) from debug container (after stopping tcpdump).\n", len(output))
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
						output, err = tryExecCat(client, namespace, podName, c, captureFilename, verbose)
						if err != nil {
							if verbose {
								fmt.Printf("Failed to cat file from app container: %v\n", err)
							}
						} else {
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
							output, err = tryExecCat(client, namespace, podName, c, captureFilename, verbose)
							if err != nil {
								if verbose {
									fmt.Printf("Failed to cat file from debug container: %v\n", err)
								}
							} else {
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
		return fmt.Errorf("failed to retrieve capture file from any container")
	}

	fmt.Printf("Successfully retrieved capture data (%d bytes).\n", len(output))
	
	// Save the file
	return SaveCaptureToFile(outputFile, output, verbose)
}
