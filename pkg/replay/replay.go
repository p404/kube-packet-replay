package replay

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/p404/kube-packet-replay/pkg/k8s"
	outpkg "github.com/p404/kube-packet-replay/pkg/output"
)

// DebugImage is the container image used for network debugging
const DebugImage = "nicolaka/netshoot:latest"

// ReplayOptions contains configuration options for packet replay
type ReplayOptions struct {
	// NetworkInterface specifies which interface to replay packets on (default: "lo")
	NetworkInterface string
	// SpeedMultiplier controls replay speed (default: 1.0 = original speed)
	SpeedMultiplier float64
	// LoopCount specifies how many times to replay the capture (default: 1)
	LoopCount int
}

// copyFileToPod copies a file from the local filesystem to a container in a pod using kubectl cp
func copyFileToPod(localFilePath, namespace, podName, containerName, remotePath string) error {
	// Build kubectl cp command
	cmd := exec.Command("kubectl", "cp", localFilePath, 
		fmt.Sprintf("%s/%s:%s", namespace, podName, remotePath),
		"-c", containerName)
	
	// Capture output and errors
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	
	// Execute the command
	err := cmd.Run()
	if err != nil {
		return fmt.Errorf("kubectl cp failed: %v, stderr: %s", err, stderr.String())
	}
	
	return nil
}

// deleteDebugContainer deletes a debug container from a pod using kubectl
func deleteDebugContainer(client *k8s.Client, namespace, podName, containerName string) error {
	// Build kubectl delete command to remove the ephemeral container
	// Since there's no direct way to delete an ephemeral container, we need to use kubectl debug --attach=false
	// and then terminate that process
	cmd := exec.Command("kubectl", "delete", "pod", 
		fmt.Sprintf("%s-debug", podName),
		"-n", namespace)
	
	// Add kubeconfig if specified
	if client.ConfigPath != "" {
		cmd.Args = append(cmd.Args, "--kubeconfig", client.ConfigPath)
	}
	
	// Capture output and errors
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	
	// Execute the command
	err := cmd.Run()
	// If the pod doesn't exist, that's ok
	if err != nil && !strings.Contains(stderr.String(), "not found") {
		return fmt.Errorf("failed to delete debug container: %v, stderr: %s", err, stderr.String())
	}
	
	return nil
}

// ReplayPackets replays network packets in a Kubernetes pod
func ReplayPackets(client *k8s.Client, namespace, podName, containerName, inputFile string, opts *ReplayOptions) error {
	var err error
	out := outpkg.Default()

	// Apply default options if not specified
	if opts == nil {
		opts = &ReplayOptions{}
	}
	
	// Set default values for unspecified options
	if opts.NetworkInterface == "" {
		opts.NetworkInterface = "lo"
	}
	if opts.SpeedMultiplier <= 0 {
		opts.SpeedMultiplier = 1.0
	}
	if opts.LoopCount <= 0 {
		opts.LoopCount = 1
	}
	

	
	// Show starting message with date
	currentTime := time.Now().Format("2006-01-02 15:04:05")
	out.Print("\n%s %s\n\n", 
		out.(*outpkg.ConsoleWriter).FormatBold("KUBE-PACKET-REPLAY REPLAY STARTED AT:"), 
		out.(*outpkg.ConsoleWriter).Colorize(outpkg.ColorBlue, currentTime))
	
	// Step 1: Setup and validation
	out.Step(1, "Setting up packet replay")
	
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
		out.(*outpkg.ConsoleWriter).Colorize(outpkg.ColorBlue, "Source file"),
		out.(*outpkg.ConsoleWriter).FormatBold(inputFile))
	out.Print("  %s: %s\n", 
		out.(*outpkg.ConsoleWriter).Colorize(outpkg.ColorBlue, "File size"), 
		out.(*outpkg.ConsoleWriter).FormatBold(fmt.Sprintf("%d bytes", fileStat.Size())))
	out.Print("  %s: %s/%s\n", 
		out.(*outpkg.ConsoleWriter).Colorize(outpkg.ColorBlue, "Target"),
		out.(*outpkg.ConsoleWriter).FormatBold(podName),
		out.(*outpkg.ConsoleWriter).FormatBold(containerName))
	out.Print("  %s: %s\n", 
		out.(*outpkg.ConsoleWriter).Colorize(outpkg.ColorBlue, "Namespace"), 
		out.(*outpkg.ConsoleWriter).FormatBold(namespace))

	// Create a debug container name with timestamp to avoid collisions
	timestamp := time.Now().Unix()
	debugContainerName := fmt.Sprintf("replay-%s-%d", containerName, timestamp)
	if containerName == "" {
		debugContainerName = fmt.Sprintf("replay-%d", timestamp)
	}
	
	// Setup cleanup function to ensure the debug container is removed even if errors occur
	cleanupDone := false
	defer func() {
		if !cleanupDone {
			// Only print message if this wasn't already handled
			out.Println()
			out.Step(4, "Emergency cleanup")
			out.Println("  Removing debug container due to error...")
			
			// Best effort cleanup - ignore errors
			_ = deleteDebugContainer(client, namespace, podName, debugContainerName)
			out.Success("  Cleanup complete")
		}
	}()
	
	// Create debug container with tcpreplay available
	// We'll just start it with a sleep command initially
	command := []string{
		"sh", "-c",
		"sleep 3600", // Keep container alive for 1 hour
	}
	
	out.Print("  Creating debug container %s...\n", debugContainerName)
	err = client.CreateDebugContainerWithKubectl(namespace, podName, containerName, DebugImage, command, debugContainerName)
	if err != nil {
		return fmt.Errorf("failed to create debug container: %v", err)
	}
	out.Success("  Done")
	out.Println()
	
	// Step 2: Copy PCAP file to pod
	out.Step(2, "Copying PCAP file to pod")
	
	// Determine if file is compressed
	isCompressed := strings.HasSuffix(inputFile, ".gz")
	remotePath := fmt.Sprintf("/tmp/replay-%d.pcap", timestamp)
	rawRemotePath := remotePath
	
	if isCompressed {
		out.Println("  Detected compressed PCAP file (.gz)")
		remotePath = fmt.Sprintf("/tmp/replay-%d.pcap.gz", timestamp)
		out.Print("  Will decompress to %s before replay\n", rawRemotePath)
	}
	
	// Copy the file to the debug container
	// Since we can't directly copy to the debug container with kubectl cp,
	// we'll copy to the pod and then move it
	out.Print("  Copying %s to pod...\n", inputFile)
	
	err = copyFileToPod(inputFile, namespace, podName, debugContainerName, remotePath)
	if err != nil {
		// Try copying to the main container instead
		if containerName != "" {
			err2 := copyFileToPod(inputFile, namespace, podName, containerName, remotePath)
			if err2 == nil {
				// Copy succeeded to main container, now we need to move it to debug container
				moveCmd := []string{"sh", "-c", fmt.Sprintf("cp %s /tmp/", remotePath)}
				_, _ = client.ExecInContainer(namespace, podName, containerName, moveCmd)
			} else {
				return fmt.Errorf("failed to copy file to pod: %v", err)
			}
		} else {
			return fmt.Errorf("failed to copy file to pod: %v", err)
		}
	}
	
	out.Success("  Done")
	out.Println()
	
	// Step 3: Run packet replay
	out.Step(3, "Running packet replay")
	out.Print("  Using tcpreplay on interface %s\n", out.(*outpkg.ConsoleWriter).FormatBold("lo"))
	
	// Prepare tcpreplay command
	replayCmd := ""
	
	// If file is compressed, decompress it first
	if isCompressed {
		replayCmd = fmt.Sprintf("gunzip -c %s > %s && ", remotePath, rawRemotePath)
		remotePath = rawRemotePath
	}
	
	// Build tcpreplay command with options
	replayCmd += fmt.Sprintf("tcpreplay -i %s", opts.NetworkInterface)
	
	// Add speed multiplier if not default
	if opts.SpeedMultiplier != 1.0 {
		replayCmd += fmt.Sprintf(" -x %.2f", opts.SpeedMultiplier)
	}
	
	// Add loop count if more than 1
	if opts.LoopCount > 1 {
		replayCmd += fmt.Sprintf(" -l %d", opts.LoopCount)
	}
	
	// Add the pcap file
	replayCmd += fmt.Sprintf(" %s", remotePath)
	
	// Execute the replay command
	out.Print("  Starting packet replay on interface %s...\n", out.(*outpkg.ConsoleWriter).FormatBold(opts.NetworkInterface))
	if opts.SpeedMultiplier != 1.0 {
		out.Print("  Speed multiplier: %s\n", out.(*outpkg.ConsoleWriter).FormatBold(fmt.Sprintf("%.2fx", opts.SpeedMultiplier)))
	}
	if opts.LoopCount > 1 {
		out.Print("  Loop count: %s\n", out.(*outpkg.ConsoleWriter).FormatBold(fmt.Sprintf("%d", opts.LoopCount)))
	}
	
	replayOutput, err := client.ExecInContainer(namespace, podName, debugContainerName, []string{"sh", "-c", replayCmd})
	if err != nil {
		return fmt.Errorf("failed to replay packets: %v", err)
	}
	
	// Display replay output (tcpreplay statistics)
	if replayOutput != "" {
		lines := strings.Split(strings.TrimSpace(replayOutput), "\n")
		for _, line := range lines {
			if line != "" {
				out.Print("  %s\n", line)
			}
		}
	}
	
	out.Success("  Done")
	out.Println()
	
	// Step 4: Cleanup
	out.Step(4, "Cleaning up")
	out.Println("  Removing debug container...")
	
	err = deleteDebugContainer(client, namespace, podName, debugContainerName)
	if err != nil {
		// This is not fatal, just log it
		out.Print("  Warning: Failed to remove debug container: %v\n", err)
	} else {
		cleanupDone = true // Mark cleanup as done to prevent defer from trying again
	}
	
	out.Success("  Done")
	
	// Extract packet count from tcpreplay output for summary
	packetCount := "unknown"
	if replayOutput != "" && strings.Contains(replayOutput, "packets") {
		// Parse tcpreplay output to get packet count
		lines := strings.Split(replayOutput, "\n")
		for _, line := range lines {
			if strings.Contains(line, "packets") {
				// Try to extract the number
				parts := strings.Fields(line)
				if len(parts) > 0 {
					packetCount = parts[0]
					break
				}
			}
		}
	}
	
	out.Print("\nPacket replay completed successfully: %s packets replayed to pod %s/%s\n", 
		out.(*outpkg.ConsoleWriter).Colorize(outpkg.ColorGreen, packetCount),
		out.(*outpkg.ConsoleWriter).FormatBold(namespace),
		out.(*outpkg.ConsoleWriter).FormatBold(podName))
	
	return nil
}