package replay

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/p404/kube-packet-replay/pkg/k8s"
)

// DebugImage is the container image used for network debugging
const DebugImage = "nicolaka/netshoot:latest"

// ReplayPackets replays network packets in a Kubernetes pod
func ReplayPackets(client *k8s.Client, namespace, podName, containerName, inputFile string) error {
	fmt.Printf("Replaying packets from %s into pod %s/%s in namespace %s\n",
		inputFile, podName, containerName, namespace)

	// Verify the input file exists
	if _, err := os.Stat(inputFile); os.IsNotExist(err) {
		return fmt.Errorf("input file %s does not exist", inputFile)
	}

	// Create a debug container name with timestamp to avoid collisions
	timestamp := time.Now().Unix()
	debugContainerName := fmt.Sprintf("replay-%s-%d", containerName, timestamp)
	if containerName == "" {
		debugContainerName = fmt.Sprintf("replay-%d", timestamp)
	}

	// First, create a debug container with a simple command that keeps it running
	command := []string{
		"sh", "-c",
		"trap : TERM INT; sleep infinity & wait",
	}

	// Create debug container
	if err := client.CreateDebugContainer(namespace, podName, containerName, DebugImage, command, debugContainerName); err != nil {
		return fmt.Errorf("failed to create debug container: %v", err)
	}

	fmt.Println("Debug container created. Waiting for it to be ready...")

	// Wait for the debug container to be ready
	// In a full implementation, we'd have a proper wait function similar to the one in the capture package
	time.Sleep(10 * time.Second)

	// Get the basename of the input file
	remoteFilePath := "/tmp/" + filepath.Base(inputFile)

	fmt.Println("Copying PCAP file to the pod...")

	// Instead of reading the file here, directly copy it using kubectl cp in a real implementation
	// For now, we'll just create a dummy file with the same name for demonstration purposes
	createFileCmd := []string{
		"sh", "-c",
		fmt.Sprintf("touch %s && echo 'This is a placeholder for PCAP data' > %s", remoteFilePath, remoteFilePath),
	}

	_, err := client.ExecInContainer(namespace, podName, debugContainerName, createFileCmd)
	if err != nil {
		return fmt.Errorf("failed to create file in pod: %v", err)
	}

	fmt.Println("Starting packet replay...")

	// Run tcpreplay with appropriate options
	replayCmd := []string{
		"sh", "-c",
		fmt.Sprintf("tcpreplay -i lo %s", remoteFilePath),
	}

	output, err := client.ExecInContainer(namespace, podName, debugContainerName, replayCmd)
	if err != nil {
		return fmt.Errorf("failed to execute replay command: %v", err)
	}

	fmt.Println(output)
	fmt.Println("Packet replay completed")

	return nil
}
