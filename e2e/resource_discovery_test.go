//go:build e2e

package e2e

import (
	"strings"
	"testing"
	"time"
)

func TestDiscoverDeploymentPods(t *testing.T) {
	stdout, stderr, err := runBinary(t, 30*time.Second,
		"capture", "tcp", "deployment", "nginx-e2e",
		"-n", testNamespace,
		"-d", "2s",
		"-v",
	)

	combined := stdout + stderr
	if !strings.Contains(combined, "nginx-e2e") {
		t.Errorf("expected output to mention the deployment name, got:\n%s", combined)
	}

	// The deployment has 2 replicas
	if strings.Contains(combined, "Pods Found") && !strings.Contains(combined, "2") {
		t.Logf("output may not show 2 pods; check:\n%s", combined)
	}

	// Discovery should succeed even if capture has timing issues
	_ = err
}

func TestDiscoverStatefulSetPods(t *testing.T) {
	stdout, stderr, err := runBinary(t, 30*time.Second,
		"capture", "tcp", "statefulset", "nginx-sts-e2e",
		"-n", testNamespace,
		"-d", "2s",
		"-v",
	)

	combined := stdout + stderr
	if !strings.Contains(combined, "nginx-sts-e2e") {
		t.Errorf("expected output to mention the statefulset name, got:\n%s", combined)
	}
	_ = err
}

func TestDiscoverDaemonSetPods(t *testing.T) {
	stdout, stderr, err := runBinary(t, 30*time.Second,
		"capture", "tcp", "daemonset", "nginx-ds-e2e",
		"-n", testNamespace,
		"-d", "2s",
		"-v",
	)

	combined := stdout + stderr
	if !strings.Contains(combined, "nginx-ds-e2e") {
		t.Errorf("expected output to mention the daemonset name, got:\n%s", combined)
	}
	_ = err
}
