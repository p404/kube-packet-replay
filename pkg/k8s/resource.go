package k8s

import (
	"context"
	"fmt"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ResourceType represents different Kubernetes resource types
type ResourceType string

const (
	// ResourceTypeDeployment represents a Kubernetes Deployment
	ResourceTypeDeployment ResourceType = "deployment"
	// ResourceTypeStatefulSet represents a Kubernetes StatefulSet
	ResourceTypeStatefulSet ResourceType = "statefulset"
	// ResourceTypeDaemonSet represents a Kubernetes DaemonSet
	ResourceTypeDaemonSet ResourceType = "daemonset"
	// ResourceTypePod represents a Kubernetes Pod
	ResourceTypePod ResourceType = "pod"
	// ResourceTypeUnknown represents an unknown or unsupported resource type
	ResourceTypeUnknown ResourceType = "unknown"
)

// ResourceInfo contains information about a discovered Kubernetes resource
type ResourceInfo struct {
	// Type is the type of resource (deployment, statefulset, etc.)
	Type ResourceType
	// Name is the name of the resource
	Name string
	// PodNames is a list of pod names associated with this resource
	PodNames []string
}

// GetPodsFromResource finds all pods associated with a given resource
// The resourceName can be a pod name, deployment name, statefulset name, etc.
// If a specific type is not specified, it will automatically detect the resource type
func (c *Client) GetPodsFromResource(namespace, resourceName string) (*ResourceInfo, error) {
	// Context with a reasonable timeout
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// First try to find it as a pod - directly return if it is a pod
	pod, err := c.ClientSet.CoreV1().Pods(namespace).Get(ctx, resourceName, metav1.GetOptions{})
	if err == nil {
		return &ResourceInfo{
			Type:     ResourceTypePod,
			Name:     pod.Name,
			PodNames: []string{pod.Name},
		}, nil
	} else {
		fmt.Printf("Resource '%s' is not a pod: %v\n", resourceName, err)
	}

	// If not a pod, try to find it as a deployment
	fmt.Printf("Checking if '%s' is a deployment...\n", resourceName)
	deployment, err := c.ClientSet.AppsV1().Deployments(namespace).Get(ctx, resourceName, metav1.GetOptions{})
	if err == nil {
		fmt.Printf("Found deployment '%s'\n", resourceName)
		// Get the selector for this deployment
		selector := metav1.FormatLabelSelector(deployment.Spec.Selector)
		fmt.Printf("Using label selector: '%s'\n", selector)
		
		// List pods matching the deployment selector
		pods, err := c.ClientSet.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
			LabelSelector: selector,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to list pods for deployment: %v", err)
		}
		
		// Log the number of pods found
		fmt.Printf("Found %d pods for deployment '%s' with selector '%s'\n", 
			len(pods.Items), resourceName, selector)

		// Extract pod names
		podNames := make([]string, 0, len(pods.Items))
		for _, pod := range pods.Items {
			podNames = append(podNames, pod.Name)
		}

		return &ResourceInfo{
			Type:     ResourceTypeDeployment,
			Name:     deployment.Name,
			PodNames: podNames,
		}, nil
	}

	// Try to find it as a statefulset
	statefulset, err := c.ClientSet.AppsV1().StatefulSets(namespace).Get(ctx, resourceName, metav1.GetOptions{})
	if err == nil {
		// Get the selector for this statefulset
		selector := metav1.FormatLabelSelector(statefulset.Spec.Selector)
		
		// List pods matching the statefulset selector
		pods, err := c.ClientSet.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
			LabelSelector: selector,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to list pods for statefulset: %v", err)
		}

		// Extract pod names
		podNames := make([]string, 0, len(pods.Items))
		for _, pod := range pods.Items {
			podNames = append(podNames, pod.Name)
		}

		return &ResourceInfo{
			Type:     ResourceTypeStatefulSet,
			Name:     statefulset.Name,
			PodNames: podNames,
		}, nil
	}

	// Try to find it as a daemonset
	daemonset, err := c.ClientSet.AppsV1().DaemonSets(namespace).Get(ctx, resourceName, metav1.GetOptions{})
	if err == nil {
		// Get the selector for this daemonset
		selector := metav1.FormatLabelSelector(daemonset.Spec.Selector)
		
		// List pods matching the daemonset selector
		pods, err := c.ClientSet.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
			LabelSelector: selector,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to list pods for daemonset: %v", err)
		}

		// Extract pod names
		podNames := make([]string, 0, len(pods.Items))
		for _, pod := range pods.Items {
			podNames = append(podNames, pod.Name)
		}

		return &ResourceInfo{
			Type:     ResourceTypeDaemonSet,
			Name:     daemonset.Name,
			PodNames: podNames,
		}, nil
	}

	// If we get here, the direct resource matching didn't work
	// Try a fallback approach - look for pods that have names starting with the resource name
	// This can help in cases where the deployment name doesn't exactly match what Kubernetes has
	// or when users refer to resources by a common name prefix
	fmt.Printf("Standard resource lookup failed, trying name-prefix fallback for '%s'\n", resourceName)
	
	// List all pods in the namespace
	pods, err := c.ClientSet.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list pods in namespace: %v", err)
	}
	
	// Look for pods with a name prefix matching our resource name
	prefixToMatch := resourceName + "-"
	var matchingPods []string
	
	for _, pod := range pods.Items {
		if strings.HasPrefix(pod.Name, prefixToMatch) {
			matchingPods = append(matchingPods, pod.Name)
		}
	}
	
	fmt.Printf("Found %d pods with name prefix '%s'\n", len(matchingPods), prefixToMatch)
	
	if len(matchingPods) > 0 {
		// We found some pods with matching prefix, assume this is a deployment
		return &ResourceInfo{
			Type:     ResourceTypeDeployment, // Treat as deployment for UI purposes
			Name:     resourceName,
			PodNames: matchingPods,
		}, nil
	}
	
	// If we get here, we couldn't find the resource using any method
	return nil, fmt.Errorf("resource '%s' not found or type not supported", resourceName)
}

// GetPodContainers returns a list of container names for a given pod
func (c *Client) GetPodContainers(namespace, podName string) ([]string, error) {
	// Context with a reasonable timeout
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Get the pod
	pod, err := c.ClientSet.CoreV1().Pods(namespace).Get(ctx, podName, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get pod %s: %v", podName, err)
	}

	// Extract container names
	containerNames := make([]string, 0, len(pod.Spec.Containers))
	for _, container := range pod.Spec.Containers {
		containerNames = append(containerNames, container.Name)
	}

	return containerNames, nil
}

// GetDefaultContainer returns the preferred container name from a pod, or empty string if none exist
// It will try to identify the main application container by applying heuristics to avoid
// selecting sidecar containers like linkerd-proxy, istio-proxy, etc.
func (c *Client) GetDefaultContainer(namespace, podName string) (string, error) {
	// Context with a reasonable timeout
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Get the pod details to see container names and annotations
	pod, err := c.ClientSet.CoreV1().Pods(namespace).Get(ctx, podName, metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("failed to get pod %s: %v", podName, err)
	}

	if len(pod.Spec.Containers) == 0 {
		return "", fmt.Errorf("no containers found in pod %s", podName)
	}

	// Create a list of known sidecar container names to avoid
	knownSidecars := map[string]bool{
		"linkerd-proxy": true,
		"istio-proxy": true,
		"envoy": true,
		"jaeger-agent": true,
		"fluentd": true,
		"datadog-agent": true,
		"prometheus-agent": true,
		"metrics-agent": true,
		"logging-agent": true,
		"sidecar": true,
	}

	// First pass: look for a container that has the same name or prefix as the pod
	// This is a common pattern where the main container name is related to the pod name
	podNameBase := podName
	// Trim any hash suffix from the pod name (for deployments)
	if parts := strings.Split(podName, "-"); len(parts) > 2 {
		// Remove the last two components which are typically hash and instance ID
		podNameBase = strings.Join(parts[:len(parts)-2], "-")
	}

	for _, container := range pod.Spec.Containers {
		// If the container name matches or is a prefix of the pod name base, it's likely the main container
		if strings.HasPrefix(podNameBase, container.Name) || strings.HasPrefix(container.Name, podNameBase) {
			return container.Name, nil
		}
	}

	// Second pass: return the first container that isn't a known sidecar
	for _, container := range pod.Spec.Containers {
		if !knownSidecars[container.Name] {
			return container.Name, nil
		}
	}

	// Fallback: return the first container if all else fails
	return pod.Spec.Containers[0].Name, nil
}

// GetResourceLabelSelector returns the label selector for the specified resource
func (c *Client) GetResourceLabelSelector(namespace, resourceName string, resourceType ResourceType) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	switch resourceType {
	case ResourceTypeDeployment:
		deployment, err := c.ClientSet.AppsV1().Deployments(namespace).Get(ctx, resourceName, metav1.GetOptions{})
		if err != nil {
			return "", fmt.Errorf("failed to get deployment %s: %v", resourceName, err)
		}
		return metav1.FormatLabelSelector(deployment.Spec.Selector), nil

	case ResourceTypeStatefulSet:
		statefulset, err := c.ClientSet.AppsV1().StatefulSets(namespace).Get(ctx, resourceName, metav1.GetOptions{})
		if err != nil {
			return "", fmt.Errorf("failed to get statefulset %s: %v", resourceName, err)
		}
		return metav1.FormatLabelSelector(statefulset.Spec.Selector), nil

	case ResourceTypeDaemonSet:
		daemonset, err := c.ClientSet.AppsV1().DaemonSets(namespace).Get(ctx, resourceName, metav1.GetOptions{})
		if err != nil {
			return "", fmt.Errorf("failed to get daemonset %s: %v", resourceName, err)
		}
		return metav1.FormatLabelSelector(daemonset.Spec.Selector), nil

	default:
		return "", fmt.Errorf("unsupported resource type: %s", resourceType)
	}
}

// FormatResourceName formats a resource name with its type for display
func FormatResourceName(resourceType ResourceType, resourceName string) string {
	return fmt.Sprintf("%s/%s", strings.ToLower(string(resourceType)), resourceName)
}
