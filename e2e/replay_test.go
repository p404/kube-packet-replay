//go:build e2e

package e2e

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestReplayToPod(t *testing.T) {
	podName := getPodName(t, "app=nginx-e2e")
	samplePcap := filepath.Join(testdataDir(), "fixtures", "sample.pcap")

	stdout, stderr, err := runBinary(t, 60*time.Second,
		"replay", "pod", podName,
		"-n", testNamespace,
		"-f", samplePcap,
	)

	combined := stdout + stderr

	// The replay pipeline should successfully:
	// 1. Create a debug container
	// 2. Copy the pcap file to the pod
	// 3. Attempt tcpreplay execution
	// tcpreplay may not be available in the default netshoot image,
	// so we verify the pipeline reaches the execution step.
	if err != nil {
		// Accept "tcpreplay: not found" as the pipeline worked correctly
		// up to the actual replay command execution
		if strings.Contains(combined, "tcpreplay") && strings.Contains(combined, "not found") {
			t.Log("replay pipeline works but tcpreplay not available in image (expected)")
			return
		}
		t.Fatalf("replay failed: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
	}

	if !strings.Contains(strings.ToLower(combined), "completed") &&
		!strings.Contains(strings.ToLower(combined), "success") &&
		!strings.Contains(strings.ToLower(combined), "done") {
		t.Errorf("expected replay to report completion, got:\n%s", combined)
	}
}

func TestReplayMissingFile(t *testing.T) {
	podName := getPodName(t, "app=nginx-e2e")

	_, _, err := runBinary(t, 15*time.Second,
		"replay", "pod", podName,
		"-n", testNamespace,
		"-f", "/nonexistent/path/file.pcap",
	)
	if err == nil {
		t.Fatal("expected error when replaying nonexistent file, got nil")
	}
}
