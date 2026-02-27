package capture

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/p404/kube-packet-replay/pkg/k8s"
	outpkg "github.com/p404/kube-packet-replay/pkg/output"
)

// tryExecCat attempts to get a file using kubectl exec cat
func tryExecCat(client *k8s.Client, namespace, podName, containerName, filePath string, verbose bool) (string, error) {
	if verbose {
		out := outpkg.Default()
		out.Debug("Executing: kubectl exec -n %s -c %s %s -- cat %s",
			namespace, containerName, podName, filePath)
	}

	// Use kubectl exec to cat the file
	cmd := []string{"cat", filePath}
	output, err := client.ExecInContainer(namespace, podName, containerName, cmd)

	if err != nil {
		return "", fmt.Errorf("failed to cat file: %v", err)
	}

	return output, nil
}

// tryKillTcpdump attempts to kill the tcpdump process in a container
func tryKillTcpdump(client *k8s.Client, namespace, podName, containerName string, verbose bool) error {
	if verbose {
		out := outpkg.Default()
		out.Debug("Killing tcpdump in container %s of pod %s", containerName, podName)
	}

	// Use exec.Command with separate arguments to avoid shell injection
	args := []string{"exec", "-n", namespace, "-c", containerName, podName, "--", "pkill", "tcpdump"}
	cmd := exec.Command("kubectl", args...)

	// Add kubeconfig if specified
	if client.ConfigPath != "" {
		cmd.Args = append([]string{"kubectl", "--kubeconfig", client.ConfigPath}, args...)
	}

	_ = cmd.Run() // Ignore errors, tcpdump might not be running

	return nil
}

// extractContainersFromPod gets container names from a pod using the Kubernetes API
func extractContainersFromPod(client *k8s.Client, namespace, podName string) []string {
	containers, err := client.GetPodContainers(namespace, podName)
	if err != nil {
		return []string{}
	}
	return containers
}

// checkGzipRunning checks if gzip is running in the container
func checkGzipRunning(client *k8s.Client, namespace, podName, containerName string) bool {
	cmd := []string{"sh", "-c", "ps aux | grep -v grep | grep -c 'gzip'"}
	output, err := client.ExecInContainer(namespace, podName, containerName, cmd)

	if err == nil && strings.TrimSpace(output) != "0" {
		return true
	}

	return false
}

// getContainerLogs gets the last few lines of container logs
func getContainerLogs(client *k8s.Client, namespace, podName, containerName string, lines int) string {
	// Use exec.Command with separate arguments to avoid shell injection
	args := []string{"logs", "-n", namespace, "-c", containerName, podName,
		"--tail", fmt.Sprintf("%d", lines)}

	cmd := exec.Command("kubectl", args...)

	// Add kubeconfig if specified
	if client.ConfigPath != "" {
		cmd.Args = append([]string{"kubectl", "--kubeconfig", client.ConfigPath}, args...)
	}

	output, err := cmd.Output()
	if err != nil {
		return ""
	}

	return string(output)
}
