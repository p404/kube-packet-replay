package k8s

import (
	"fmt"
	"os"
	"path/filepath"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
)

// Client is a wrapper around the Kubernetes clientset
type Client struct {
	ClientSet  kubernetes.Interface
	Config     *rest.Config
	ConfigPath string // Path to kubeconfig file
}

// NewClient creates a new Kubernetes client
func NewClient(kubeconfigPath string) (*Client, error) {
	kubeconfig := kubeconfigPath
	if kubeconfig == "" {
		if home := homedir.HomeDir(); home != "" {
			kubeconfig = filepath.Join(home, ".kube", "config")
		}
	}

	var config *rest.Config

	// Check if kubeconfig exists on disk
	if _, statErr := os.Stat(kubeconfig); os.IsNotExist(statErr) {
		// Try in-cluster config
		var err error
		config, err = rest.InClusterConfig()
		if err != nil {
			return nil, fmt.Errorf("failed to create in-cluster config (kubeconfig not found at %s): %v", kubeconfig, err)
		}
	} else {
		// Load kubeconfig from file
		var err error
		config, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
		if err != nil {
			return nil, fmt.Errorf("failed to load kubeconfig from %s: %v", kubeconfig, err)
		}
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create Kubernetes clientset: %v", err)
	}

	return &Client{
		ClientSet:  clientset,
		Config:     config,
		ConfigPath: kubeconfig,
	}, nil
}
