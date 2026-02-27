//go:build e2e

package e2e

import (
	"strings"
	"testing"
	"time"
)

func TestInvalidResourceType(t *testing.T) {
	_, stderr, err := runBinary(t, 10*time.Second,
		"capture", "tcp", "cronjob", "my-job",
		"-n", testNamespace,
	)
	if err == nil {
		t.Fatal("expected error for invalid resource type")
	}
	if !strings.Contains(strings.ToLower(stderr), "invalid resource type") &&
		!strings.Contains(strings.ToLower(stderr), "must be one of") {
		t.Errorf("expected helpful error message about invalid resource type, got: %s", stderr)
	}
}

func TestNonExistentPod(t *testing.T) {
	_, stderr, err := runBinary(t, 15*time.Second,
		"capture", "tcp", "pod", "nonexistent-pod-12345",
		"-n", testNamespace,
		"-d", "2s",
	)
	if err == nil {
		t.Fatal("expected error for nonexistent pod")
	}
	if !strings.Contains(strings.ToLower(stderr), "not found") &&
		!strings.Contains(strings.ToLower(stderr), "error") {
		t.Errorf("expected 'not found' in error, got: %s", stderr)
	}
}

func TestNonExistentDeployment(t *testing.T) {
	_, stderr, err := runBinary(t, 15*time.Second,
		"capture", "tcp", "deployment", "nonexistent-deploy",
		"-n", testNamespace,
		"-d", "2s",
	)
	if err == nil {
		t.Fatal("expected error for nonexistent deployment")
	}
	if !strings.Contains(strings.ToLower(stderr), "not found") &&
		!strings.Contains(strings.ToLower(stderr), "error") {
		t.Errorf("expected error output for nonexistent deployment, got: %s", stderr)
	}
}

func TestInvalidFilter(t *testing.T) {
	podName := getPodName(t, "app=nginx-e2e")

	_, stderr, err := runBinary(t, 10*time.Second,
		"capture", "tcp; rm -rf /", "pod", podName,
		"-n", testNamespace,
	)
	if err == nil {
		t.Fatal("expected error for malicious filter expression")
	}
	if !strings.Contains(strings.ToLower(stderr), "prohibited") &&
		!strings.Contains(strings.ToLower(stderr), "invalid") &&
		!strings.Contains(strings.ToLower(stderr), "dangerous") {
		t.Errorf("expected filter validation error, got: %s", stderr)
	}
}

func TestMissingArgs(t *testing.T) {
	_, _, err := runBinary(t, 10*time.Second,
		"capture",
	)
	if err == nil {
		t.Fatal("expected error for missing arguments")
	}
}

func TestReplayMissingFileFlag(t *testing.T) {
	_, _, err := runBinary(t, 10*time.Second,
		"replay", "pod", "some-pod",
		"-n", testNamespace,
	)
	if err == nil {
		t.Fatal("expected error when -f flag is missing")
	}
}

func TestVersionCommand(t *testing.T) {
	stdout, stderr, err := runBinary(t, 10*time.Second, "version")
	if err != nil {
		t.Fatalf("version command failed: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
	}
	combined := stdout + stderr
	if !strings.Contains(strings.ToLower(combined), "kube-packet-replay") {
		t.Errorf("expected version output to contain 'kube-packet-replay', got: %s", combined)
	}
}
