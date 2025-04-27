package k8s

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/remotecommand"
)

// CreateDebugContainer creates an ephemeral debug container in the specified pod
func (c *Client) CreateDebugContainer(namespace, podName, containerName, debugImage string, command []string, debugContainerName string) error {
	// Get the pod
	pod, err := c.ClientSet.CoreV1().Pods(namespace).Get(context.TODO(), podName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to get pod %s: %v", podName, err)
	}

	// If no debugContainerName is provided, create a default one
	if debugContainerName == "" {
		debugContainerName = fmt.Sprintf("debug-%s", containerName)
		if containerName == "" {
			debugContainerName = "debug"
		}
	}

	// Setup ephemeral container
	ec := corev1.EphemeralContainer{
		EphemeralContainerCommon: corev1.EphemeralContainerCommon{
			Name:            debugContainerName,
			Image:           debugImage,
			ImagePullPolicy: corev1.PullIfNotPresent,
			Command:         command,
			// Share the target container's process namespace
			SecurityContext: &corev1.SecurityContext{
				Capabilities: &corev1.Capabilities{
					Add: []corev1.Capability{"NET_ADMIN", "SYS_PTRACE"},
				},
			},
		},
		TargetContainerName: containerName,
	}

	// Use patch operation to add ephemeral container
	pod.Spec.EphemeralContainers = append(pod.Spec.EphemeralContainers, ec)

	// Update the pod to add the ephemeral container
	_, err = c.ClientSet.CoreV1().Pods(namespace).UpdateEphemeralContainers(
		context.TODO(),
		podName,
		pod,
		metav1.UpdateOptions{},
	)
	if err != nil {
		return fmt.Errorf("failed to add ephemeral container: %v", err)
	}

	return nil
}

// ExecInContainer executes a command in a container and returns the output
func (c *Client) ExecInContainer(namespace, podName, containerName string, command []string) (string, error) {
	// Try using kubectl directly first when a container name is specified
	if containerName != "" {
		// Build the kubectl command with quoted arguments to handle spaces
		kubectlArgs := []string{"exec"}
		
		// Add namespace
		kubectlArgs = append(kubectlArgs, "-n", namespace)
		
		// Add container name
		kubectlArgs = append(kubectlArgs, "-c", containerName)
		
		// Add pod name
		kubectlArgs = append(kubectlArgs, podName)
		
		// Add command with -- separator
		kubectlArgs = append(kubectlArgs, "--")
		kubectlArgs = append(kubectlArgs, command...)
		
		// Execute kubectl command
		cmd := exec.Command("kubectl")
		
		// Add kubeconfig if specified
		if c.ConfigPath != "" {
			cmd.Args = append(cmd.Args, "--kubeconfig", c.ConfigPath)
		}
		
		// Add the arguments
		cmd.Args = append(cmd.Args, kubectlArgs...)
		
		// Capture output and errors
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		
		// Execute the command
		err := cmd.Run()
		if err == nil {
			// Command succeeded
			return stdout.String(), nil
		}
		
		// If the error mentions "choose one of:" then return that error to help with container detection
		if strings.Contains(stderr.String(), "choose one of:") {
			return "", fmt.Errorf("failed to execute command: %v, stderr: %s", err, stderr.String())
		}
		
		// For other errors, try falling back to the API method
		// But don't fall back if this was already us trying all containers systematically
	}
	
	// Use the Kubernetes API method
	req := c.ClientSet.CoreV1().RESTClient().Post().
		Resource("pods").
		Name(podName).
		Namespace(namespace).
		SubResource("exec")

	req.VersionedParams(&corev1.PodExecOptions{
		Container: containerName,
		Command:   command,
		Stdin:     false,
		Stdout:    true,
		Stderr:    true,
		TTY:       false,
	}, metav1.ParameterCodec)

	// Execute the command
	var stdout, stderr bytes.Buffer
	exec, err := remotecommand.NewSPDYExecutor(c.Config, "POST", req.URL())
	if err != nil {
		return "", err
	}

	err = exec.Stream(remotecommand.StreamOptions{
		Stdout: &stdout,
		Stderr: &stderr,
	})

	if err != nil {
		// Extract container names from the error if possible
		errorMsg := stderr.String()
		if strings.Contains(errorMsg, "choose one of:") {
			return "", fmt.Errorf("failed to execute command: %v, stderr: %s", err, errorMsg)
		}
		return "", fmt.Errorf("failed to execute command: %v, stderr: %s", err, errorMsg)
	}

	return stdout.String(), nil
}

// GetPodLogs gets logs from a container in a pod
func (c *Client) GetPodLogs(namespace, podName, containerName string, follow bool) (io.ReadCloser, error) {
	req := c.ClientSet.CoreV1().Pods(namespace).GetLogs(podName, &corev1.PodLogOptions{
		Container: containerName,
		Follow:    follow,
	})

	return req.Stream(context.TODO())
}
