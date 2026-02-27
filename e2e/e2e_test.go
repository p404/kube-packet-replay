//go:build e2e

package e2e

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

var (
	binaryPath    string
	kubeconfig    string
	testNamespace = "kpr-e2e-test"
)

func TestMain(m *testing.M) {
	// 1. Determine binary path (allow override via KPR_BINARY)
	binaryPath = os.Getenv("KPR_BINARY")
	if binaryPath == "" {
		// Build the binary to a temp location
		tmpDir, err := os.MkdirTemp("", "kpr-e2e-*")
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to create temp dir: %v\n", err)
			os.Exit(1)
		}
		defer os.RemoveAll(tmpDir)

		binaryPath = filepath.Join(tmpDir, "kube-packet-replay")
		cmd := exec.Command("go", "build", "-o", binaryPath, "../main.go")
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "failed to build binary: %v\n", err)
			os.Exit(1)
		}
	}

	// Verify binary exists
	if _, err := os.Stat(binaryPath); err != nil {
		fmt.Fprintf(os.Stderr, "binary not found at %s: %v\n", binaryPath, err)
		os.Exit(1)
	}

	// 2. Determine kubeconfig
	kubeconfig = os.Getenv("KUBECONFIG")
	if kubeconfig == "" {
		kubeconfig = filepath.Join(os.Getenv("HOME"), ".kube", "config")
	}

	// 3. Verify cluster connectivity
	if err := verifyCluster(); err != nil {
		fmt.Fprintf(os.Stderr, "cluster not reachable: %v\n", err)
		os.Exit(1)
	}

	// 4. Deploy test workloads
	if err := deployTestWorkloads(); err != nil {
		fmt.Fprintf(os.Stderr, "failed to deploy test workloads: %v\n", err)
		os.Exit(1)
	}

	// 5. Wait for all pods to be ready
	if err := waitForWorkloads(); err != nil {
		fmt.Fprintf(os.Stderr, "workloads not ready: %v\n", err)
		cleanupTestWorkloads()
		os.Exit(1)
	}

	fmt.Println("E2E test environment is ready")

	// 6. Run tests
	code := m.Run()

	// 7. Cleanup
	cleanupTestWorkloads()

	os.Exit(code)
}
