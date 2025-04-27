package k8s

import (
	"os"
	"path/filepath"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
)

// Client is a wrapper around the Kubernetes clientset
type Client struct {
	ClientSet   kubernetes.Interface
	Config      *rest.Config
	ConfigPath  string // Path to kubeconfig file
}

// NewClient creates a new Kubernetes client
func NewClient(kubeconfigPath string) (*Client, error) {
	// Get kubeconfig path
	kubeconfig := kubeconfigPath
	if kubeconfig == "" {
		if home := homedir.HomeDir(); home != "" {
			kubeconfig = filepath.Join(home, ".kube", "config")
		}
	}

	var config *rest.Config
	var err error

	// Check if kubeconfig exists
	if _, err := os.Stat(kubeconfig); os.IsNotExist(err) {
		// If not, try in-cluster config
		config, err = rest.InClusterConfig()
		if err != nil {
			return nil, err
		}
	} else {
		// Load kubeconfig
		config, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
		if err != nil {
			return nil, err
		}
	}

	// Create the clientset
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, err
	}

	return &Client{
		ClientSet:  clientset,
		Config:     config,
		ConfigPath: kubeconfig,
	}, nil
}
