package replay

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/p404/kube-packet-replay/pkg/cli"
	"github.com/p404/kube-packet-replay/pkg/k8s"
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
	fmt.Printf("\n%s %s\n\n", 
		cli.Colorize(cli.ColorBold, "KUBE-PACKET-REPLAY REPLAY STARTED AT:"), 
		cli.Colorize(cli.ColorBlue, currentTime))
	
	// Step 1: Setup and validation
	fmt.Println(cli.Step(1, "Setting up packet replay"))
	
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
	fmt.Printf("  %s: %s/%s\n", 
		cli.Colorize(cli.ColorBlue, "Target"),
		cli.Colorize(cli.ColorBold, podName),
		cli.Colorize(cli.ColorBold, containerName))
	fmt.Printf("  %s: %s\n", 
		cli.Colorize(cli.ColorBlue, "Namespace"), 
		cli.Colorize(cli.ColorBold, namespace))

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
			fmt.Println("\n" + cli.Step(4, "Emergency cleanup"))
			fmt.Println("  Removing debug container due to error...")
			
			// Best effort cleanup - ignore errors
			_ = deleteDebugContainer(client, namespace, podName, debugContainerName)
			fmt.Println("  " + cli.Success("Cleanup complete"))
		}
	}()

	// First, create a debug container with a simple command that keeps it running
	command := []string{
		"sh", "-c",
		"trap : TERM INT; sleep infinity & wait",
	}

	// Create debug container
	fmt.Printf("  Creating debug container %s...\n", debugContainerName)
	err = client.CreateDebugContainerWithKubectl(namespace, podName, containerName, DebugImage, command, debugContainerName)
	if err != nil {
		return fmt.Errorf("failed to create debug container: %v", err)
	}
	fmt.Println("  " + cli.Success("Done"))
	fmt.Println()

	// Step 2: Copy PCAP file to pod
	fmt.Println(cli.Step(2, "Copying PCAP file to pod"))
	
	// Get the basename of the input file
	remoteFilePath := "/tmp/" + filepath.Base(inputFile)
	
	// Check if we're dealing with a compressed file
	isCompressed := strings.HasSuffix(inputFile, ".gz")
	
	// Set up the command to handle the file appropriately
	if isCompressed {
		fmt.Println("  Detected compressed PCAP file (.gz)")
		rawRemotePath := strings.TrimSuffix(remoteFilePath, ".gz")
		fmt.Printf("  Will decompress to %s before replay\n", rawRemotePath)
		
		// Note: decompression will be handled after the file is copied
		
		// Update the remote path to the decompressed file for tcpreplay
		remoteFilePath = rawRemotePath
	}
	
	// Create a placeholder file that we'd replace with kubectl cp in a real scenario
	// In a real implementation, we would use kubectl cp to copy the file
	fmt.Printf("  Copying %s to pod...\n", inputFile)
	
	// Copy file to pod using kubectl cp
	err = copyFileToPod(inputFile, namespace, podName, debugContainerName, remoteFilePath)
	if err != nil {
		return fmt.Errorf("failed to copy file to pod: %v", err)
	}
	
	// If file is compressed, decompress it
	if isCompressed {
		decompressCmd := []string{
			"sh", "-c",
			fmt.Sprintf("gunzip -f %s", remoteFilePath+".gz"),
		}
		
		_, err = client.ExecInContainer(namespace, podName, debugContainerName, decompressCmd)
		if err != nil {
			return fmt.Errorf("failed to decompress file in pod: %v", err)
		}
	}
	if err != nil {
		return fmt.Errorf("failed to prepare file in pod: %v", err)
	}
	fmt.Println("  " + cli.Success("Done"))
	fmt.Println()

	// Step 3: Run packet replay
	fmt.Println(cli.Step(3, "Running packet replay"))
	fmt.Printf("  Using tcpreplay on interface %s\n", cli.Colorize(cli.ColorBold, "lo"))
	
	// Build tcpreplay command with configured options
	tcpreplayCmd := fmt.Sprintf(
		"tcpreplay --stats=1 -i %s ", 
		opts.NetworkInterface,
	)
	
	// Add speed multiplier if not 1.0
	if opts.SpeedMultiplier != 1.0 {
		tcpreplayCmd += fmt.Sprintf("--multiplier=%.2f ", opts.SpeedMultiplier)
	}
	
	// Add loop count if greater than 1
	if opts.LoopCount > 1 {
		tcpreplayCmd += fmt.Sprintf("--loop=%d ", opts.LoopCount)
	}
	
	// Add the file path
	tcpreplayCmd += remoteFilePath
	
	// Run tcpreplay with appropriate options
	replayCmd := []string{
		"sh", "-c",
		tcpreplayCmd,
	}
	
	fmt.Printf("  Starting packet replay on interface %s...\n", cli.Colorize(cli.ColorBold, opts.NetworkInterface))
	if opts.SpeedMultiplier != 1.0 {
		fmt.Printf("  Speed multiplier: %s\n", cli.Colorize(cli.ColorBold, fmt.Sprintf("%.2fx", opts.SpeedMultiplier)))
	}
	if opts.LoopCount > 1 {
		fmt.Printf("  Loop count: %s\n", cli.Colorize(cli.ColorBold, fmt.Sprintf("%d", opts.LoopCount)))
	}
	
	output, err := client.ExecInContainer(namespace, podName, debugContainerName, replayCmd)
	if err != nil {
		return fmt.Errorf("failed to execute replay command: %v", err)
	}
	
	// Show only the important parts of tcpreplay output
	outputLines := strings.Split(output, "\n")
	for _, line := range outputLines {
		if strings.Contains(line, "packets") || strings.Contains(line, "bytes") || 
		   strings.Contains(line, "rate") || strings.Contains(line, "success") {
			fmt.Printf("  %s\n", line)
		}
	}
	
	fmt.Println("  " + cli.Success("Done"))
	fmt.Println()
	
	// Step 4: Cleanup
	fmt.Println(cli.Step(4, "Cleaning up"))
	fmt.Println("  Removing debug container...")
	
	// Delete the debug container
	err = deleteDebugContainer(client, namespace, podName, debugContainerName)
	if err != nil {
		// Just log the error but don't fail the whole operation
		fmt.Printf("  Warning: Failed to remove debug container: %v\n", err)
	}
	
	// Mark cleanup as done so the defer function doesn't try to clean up again
	cleanupDone = true
	
	fmt.Println("  " + cli.Success("Done"))
	
	// Final success message
	fmt.Printf("Packet replay completed successfully: %s packets replayed to pod %s/%s\n", 
		cli.Colorize(cli.ColorBold, filepath.Base(inputFile)),
		cli.Colorize(cli.ColorBold, podName),
		cli.Colorize(cli.ColorBold, containerName))
	
	return nil
}
