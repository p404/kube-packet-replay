package capture

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"

	"github.com/p404/kube-packet-replay/pkg/k8s"
)

// tryExecCat attempts to get a file using kubectl exec cat
func tryExecCat(client *k8s.Client, namespace, podName, containerName, filePath string, verbose bool) (string, error) {
	if verbose {
		fmt.Printf("Executing: kubectl %s exec -n %s -c %s %s -- cat %s\n",
			getKubeconfigArg(client), namespace, containerName, podName, filePath)
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
	// Try to kill tcpdump process
	killCmd := fmt.Sprintf("kubectl %s exec -n %s -c %s %s -- pkill tcpdump",
		getKubeconfigArg(client), namespace, containerName, podName)

	if verbose {
		fmt.Printf("Executing: %s\n", killCmd)
	}

	cmd := exec.Command("sh", "-c", killCmd)
	_ = cmd.Run() // Ignore errors, tcpdump might not be running

	return nil
}

// extractContainersFromPod gets container names from a pod by parsing error messages
func extractContainersFromPod(client *k8s.Client, namespace, podName string) []string {
	cmd := []string{"sh", "-c", "echo 'Getting container list'"}
	_, err := client.ExecInContainer(namespace, podName, "", cmd)

	if err != nil {
		errorMsg := err.Error()
		// Check if the error contains container list
		if strings.Contains(errorMsg, "choose one of:") {
			start := strings.Index(errorMsg, "[")
			end := strings.Index(errorMsg, "]")
			if start != -1 && end != -1 && end > start {
				containerList := errorMsg[start+1 : end]
				// Split by spaces or commas
				containers := strings.FieldsFunc(containerList, func(r rune) bool {
					return r == ' ' || r == ','
				})

				var result []string
				for _, c := range containers {
					c = strings.TrimSpace(c)
					if c != "" {
						result = append(result, c)
					}
				}
				return result
			}
		}
	}

	// If we can't extract containers, return an empty list
	return []string{}
}

// getKubeconfigArg returns the kubeconfig argument if a config path is set
func getKubeconfigArg(client *k8s.Client) string {
	if client.ConfigPath != "" {
		return fmt.Sprintf("--kubeconfig %s", client.ConfigPath)
	}
	return ""
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
	logCmd := fmt.Sprintf("kubectl %s logs -n %s -c %s %s | tail -n %d",
		getKubeconfigArg(client), namespace, containerName, podName, lines)
		
	cmd := exec.Command("sh", "-c", logCmd)
	var logOutput bytes.Buffer
	cmd.Stdout = &logOutput
	cmd.Run()
	
	return logOutput.String()
}
