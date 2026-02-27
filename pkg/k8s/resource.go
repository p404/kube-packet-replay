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

// GetPodsFromResource finds all pods associated with a given resource.
// The resourceName can be a pod name, deployment name, statefulset name, etc.
// If a specific type is not specified, it will automatically detect the resource type.
func (c *Client) GetPodsFromResource(namespace, resourceName string) (*ResourceInfo, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// First try to find it as a pod
	pod, err := c.ClientSet.CoreV1().Pods(namespace).Get(ctx, resourceName, metav1.GetOptions{})
	if err == nil {
		return &ResourceInfo{
			Type:     ResourceTypePod,
			Name:     pod.Name,
			PodNames: []string{pod.Name},
		}, nil
	}

	// Try as a deployment
	deployment, err := c.ClientSet.AppsV1().Deployments(namespace).Get(ctx, resourceName, metav1.GetOptions{})
	if err == nil {
		selector := metav1.FormatLabelSelector(deployment.Spec.Selector)
		var podNames []string
		podNames, err = c.listPodNamesBySelector(ctx, namespace, selector)
		if err != nil {
			return nil, fmt.Errorf("failed to list pods for deployment: %v", err)
		}
		return &ResourceInfo{
			Type:     ResourceTypeDeployment,
			Name:     deployment.Name,
			PodNames: podNames,
		}, nil
	}

	// Try as a statefulset
	statefulset, err := c.ClientSet.AppsV1().StatefulSets(namespace).Get(ctx, resourceName, metav1.GetOptions{})
	if err == nil {
		selector := metav1.FormatLabelSelector(statefulset.Spec.Selector)
		var podNames []string
		podNames, err = c.listPodNamesBySelector(ctx, namespace, selector)
		if err != nil {
			return nil, fmt.Errorf("failed to list pods for statefulset: %v", err)
		}
		return &ResourceInfo{
			Type:     ResourceTypeStatefulSet,
			Name:     statefulset.Name,
			PodNames: podNames,
		}, nil
	}

	// Try as a daemonset
	daemonset, err := c.ClientSet.AppsV1().DaemonSets(namespace).Get(ctx, resourceName, metav1.GetOptions{})
	if err == nil {
		selector := metav1.FormatLabelSelector(daemonset.Spec.Selector)
		var podNames []string
		podNames, err = c.listPodNamesBySelector(ctx, namespace, selector)
		if err != nil {
			return nil, fmt.Errorf("failed to list pods for daemonset: %v", err)
		}
		return &ResourceInfo{
			Type:     ResourceTypeDaemonSet,
			Name:     daemonset.Name,
			PodNames: podNames,
		}, nil
	}

	// Fallback: look for pods with names starting with the resource name
	pods, err := c.ClientSet.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list pods in namespace: %v", err)
	}

	prefixToMatch := resourceName + "-"
	var matchingPods []string

	for i := range pods.Items {
		if strings.HasPrefix(pods.Items[i].Name, prefixToMatch) {
			matchingPods = append(matchingPods, pods.Items[i].Name)
		}
	}

	if len(matchingPods) > 0 {
		return &ResourceInfo{
			Type:     ResourceTypeDeployment,
			Name:     resourceName,
			PodNames: matchingPods,
		}, nil
	}

	return nil, fmt.Errorf("resource '%s' not found in namespace '%s'", resourceName, namespace)
}

// listPodNamesBySelector lists pod names matching a label selector
func (c *Client) listPodNamesBySelector(ctx context.Context, namespace, selector string) ([]string, error) {
	pods, err := c.ClientSet.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: selector,
	})
	if err != nil {
		return nil, err
	}

	podNames := make([]string, 0, len(pods.Items))
	for i := range pods.Items {
		podNames = append(podNames, pods.Items[i].Name)
	}
	return podNames, nil
}

// GetPodContainers returns a list of container names for a given pod
func (c *Client) GetPodContainers(namespace, podName string) ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pod, err := c.ClientSet.CoreV1().Pods(namespace).Get(ctx, podName, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get pod %s: %v", podName, err)
	}

	containerNames := make([]string, 0, len(pod.Spec.Containers))
	for i := range pod.Spec.Containers {
		containerNames = append(containerNames, pod.Spec.Containers[i].Name)
	}

	// Also include ephemeral containers
	for i := range pod.Spec.EphemeralContainers {
		containerNames = append(containerNames, pod.Spec.EphemeralContainers[i].Name)
	}

	return containerNames, nil
}

// GetDefaultContainer returns the preferred container name from a pod.
// It applies heuristics to avoid selecting sidecar containers.
func (c *Client) GetDefaultContainer(namespace, podName string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pod, err := c.ClientSet.CoreV1().Pods(namespace).Get(ctx, podName, metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("failed to get pod %s: %v", podName, err)
	}

	if len(pod.Spec.Containers) == 0 {
		return "", fmt.Errorf("no containers found in pod %s", podName)
	}

	// Known sidecar container names to avoid
	knownSidecars := map[string]bool{
		"linkerd-proxy":    true,
		"istio-proxy":      true,
		"envoy":            true,
		"jaeger-agent":     true,
		"fluentd":          true,
		"datadog-agent":    true,
		"prometheus-agent": true,
		"metrics-agent":    true,
		"logging-agent":    true,
		"sidecar":          true,
	}

	// First pass: look for a container with a name related to the pod name
	podNameBase := podName
	if parts := strings.Split(podName, "-"); len(parts) > 2 {
		podNameBase = strings.Join(parts[:len(parts)-2], "-")
	}

	for i := range pod.Spec.Containers {
		if strings.HasPrefix(podNameBase, pod.Spec.Containers[i].Name) || strings.HasPrefix(pod.Spec.Containers[i].Name, podNameBase) {
			return pod.Spec.Containers[i].Name, nil
		}
	}

	// Second pass: return the first non-sidecar container
	for i := range pod.Spec.Containers {
		if !knownSidecars[pod.Spec.Containers[i].Name] {
			return pod.Spec.Containers[i].Name, nil
		}
	}

	// Fallback: return the first container
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
