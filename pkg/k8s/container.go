package k8s

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/remotecommand"
)

// CreateDebugContainer creates an ephemeral debug container in the specified pod
// This implementation uses direct API calls to create ephemeral containers
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

// CreateDebugContainerWithKubectl creates an ephemeral debug container using kubectl debug
// This is the preferred method as it's more reliable and uses standard tooling
func (c *Client) CreateDebugContainerWithKubectl(namespace, podName, containerName, debugImage string, command []string, debugContainerName string) error {
	// Build kubectl debug command
	args := []string{
		"debug",
		fmt.Sprintf("pod/%s", podName),
		"-n", namespace,
		"--image", debugImage,
	}

	// Add container name if specified
	if debugContainerName != "" {
		args = append(args, "-c", debugContainerName)
	}

	// Target specific container if specified
	if containerName != "" {
		args = append(args, "--target", containerName)
	}

	// Add the profile for network admin capabilities
	args = append(args, "--profile", "netadmin")

	// Add quiet flag to reduce output
	args = append(args, "--quiet")
	
	// Add command to execute
	args = append(args, "--")
	
	// If command is a shell command with arguments, we need to format it correctly
	if len(command) > 0 {
		// Handle the common case of ["sh", "-c", "actual command"]
		if command[0] == "sh" && len(command) > 2 && command[1] == "-c" {
			args = append(args, "sh", "-c", command[2])
		} else {
			args = append(args, command...)
		}
	}

	// Execute kubectl command
	cmd := exec.Command("kubectl")
	
	// Add kubeconfig if specified
	if c.ConfigPath != "" {
		cmd.Args = append(cmd.Args, "--kubeconfig", c.ConfigPath)
	}
	
	// Add the arguments
	cmd.Args = append(cmd.Args, args...)
	
	if strings.Contains(strings.Join(cmd.Args, " "), "trap") {
		// For trap commands that keep the container running, we start in background
		// and don't wait for completion
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		
		// Start process but don't wait for it
		err := cmd.Start()
		if err != nil {
			return fmt.Errorf("failed to start debug container: %v, stderr: %s", err, stderr.String())
		}
		
		// Let the process run in background
		go func() {
			_ = cmd.Wait() // Ignore any errors from the long-running process
		}()
		
		// Wait a moment for container to start
		time.Sleep(3 * time.Second)
		
		return nil
	}
	
	// For normal commands, run and wait for completion
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	
	err := cmd.Run()
	if err != nil {
		return fmt.Errorf("failed to create debug container: %v, stderr: %s", err, stderr.String())
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
