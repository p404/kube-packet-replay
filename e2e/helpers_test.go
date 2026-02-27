//go:build e2e

package e2e

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// testdataDir returns the absolute path to the testdata directory.
func testdataDir() string {
	_, filename, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(filename), "testdata")
}

// runBinary executes the kube-packet-replay binary with given args and returns
// stdout, stderr, and the error. It uses a context with timeout.
func runBinary(t *testing.T, timeout time.Duration, args ...string) (string, string, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	allArgs := make([]string, len(args))
	copy(allArgs, args)
	// Only add --kubeconfig for commands that support it (not version, help, etc.)
	if len(args) > 0 && args[0] != "version" && args[0] != "help" {
		allArgs = append(allArgs, "--kubeconfig", kubeconfig)
	}
	cmd := exec.CommandContext(ctx, binaryPath, allArgs...)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	return stdout.String(), stderr.String(), err
}

// kubectl runs a kubectl command and returns output.
func kubectl(args ...string) (string, error) {
	allArgs := append([]string{"--kubeconfig", kubeconfig}, args...)
	cmd := exec.Command("kubectl", allArgs...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// verifyCluster checks that the cluster is reachable.
func verifyCluster() error {
	_, err := kubectl("cluster-info")
	return err
}

// deployTestWorkloads applies all manifests in the testdata/manifests directory.
func deployTestWorkloads() error {
	manifestDir := filepath.Join(testdataDir(), "manifests")
	_, err := kubectl("apply", "-f", manifestDir, "--recursive")
	return err
}

// waitForWorkloads waits for all test workloads to become ready.
func waitForWorkloads() error {
	waitTargets := []struct {
		resource string
		name     string
		timeout  string
	}{
		{"deployment", "nginx-e2e", "120s"},
		{"statefulset", "nginx-sts-e2e", "120s"},
		{"daemonset", "nginx-ds-e2e", "120s"},
	}

	for _, w := range waitTargets {
		_, err := kubectl("rollout", "status", w.resource+"/"+w.name,
			"-n", testNamespace, "--timeout="+w.timeout)
		if err != nil {
			return fmt.Errorf("timeout waiting for %s/%s: %v", w.resource, w.name, err)
		}
	}

	// Wait for the traffic-gen pod separately
	_, err := kubectl("wait", "--for=condition=ready", "pod/traffic-gen",
		"-n", testNamespace, "--timeout=60s")
	if err != nil {
		return fmt.Errorf("timeout waiting for traffic-gen pod: %v", err)
	}

	return nil
}

// cleanupTestWorkloads deletes the test namespace.
func cleanupTestWorkloads() {
	_, _ = kubectl("delete", "namespace", testNamespace, "--wait=false")
}

// getPodName returns the name of the first pod matching a label selector.
func getPodName(t *testing.T, labelSelector string) string {
	t.Helper()
	out, err := kubectl("get", "pods", "-n", testNamespace,
		"-l", labelSelector, "-o", "jsonpath={.items[0].metadata.name}")
	if err != nil {
		t.Fatalf("failed to get pod name for selector %q: %v, output: %s", labelSelector, err, out)
	}
	name := strings.TrimSpace(out)
	if name == "" {
		t.Fatalf("no pods found for selector %q", labelSelector)
	}
	return name
}

// getAllPodNames returns all pod names matching a label selector.
func getAllPodNames(t *testing.T, labelSelector string) []string {
	t.Helper()
	out, err := kubectl("get", "pods", "-n", testNamespace,
		"-l", labelSelector, "-o", "jsonpath={.items[*].metadata.name}")
	if err != nil {
		t.Fatalf("failed to get pod names for selector %q: %v, output: %s", labelSelector, err, out)
	}
	return strings.Fields(strings.TrimSpace(out))
}

// generateTraffic runs curl from the traffic-gen pod to the given service.
func generateTraffic(t *testing.T, targetURL string, count int) {
	t.Helper()
	for i := 0; i < count; i++ {
		_, err := kubectl("exec", "-n", testNamespace, "traffic-gen", "--",
			"curl", "-s", "-o", "/dev/null", "-m", "2", targetURL)
		if err != nil {
			t.Logf("traffic generation attempt %d/%d failed (may be expected early on): %v", i+1, count, err)
		}
		time.Sleep(300 * time.Millisecond)
	}
}

// fileExists checks if a file exists and is non-empty.
func fileExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.Size() > 0
}

// isPcapOrGzip checks if a file starts with pcap or gzip magic bytes.
func isPcapOrGzip(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()

	header := make([]byte, 4)
	n, err := f.Read(header)
	if err != nil || n < 2 {
		return false
	}

	// Gzip magic: 0x1f 0x8b
	if header[0] == 0x1f && header[1] == 0x8b {
		return true
	}
	// Pcap magic (little-endian): 0xd4c3b2a1
	if n >= 4 && header[0] == 0xd4 && header[1] == 0xc3 && header[2] == 0xb2 && header[3] == 0xa1 {
		return true
	}
	// Pcap magic (big-endian): 0xa1b2c3d4
	if n >= 4 && header[0] == 0xa1 && header[1] == 0xb2 && header[2] == 0xc3 && header[3] == 0xd4 {
		return true
	}
	return false
}
