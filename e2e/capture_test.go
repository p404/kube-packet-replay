//go:build e2e

package e2e

import (
	"path/filepath"
	"testing"
	"time"
)

func TestCaptureSinglePod(t *testing.T) {
	podName := getPodName(t, "app=nginx-e2e")
	tmpDir := t.TempDir()
	outputFile := filepath.Join(tmpDir, "capture-pod.pcap.gz")

	// Generate traffic concurrently during capture
	go generateTraffic(t, "http://nginx-e2e.kpr-e2e-test.svc.cluster.local", 10)

	stdout, stderr, err := runBinary(t, 60*time.Second,
		"capture", "tcp port 80", "pod", podName,
		"-n", testNamespace,
		"-d", "10s",
		"-o", outputFile,
	)
	if err != nil {
		t.Fatalf("capture failed: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
	}

	if !fileExists(outputFile) {
		t.Fatal("capture output file was not created or is empty")
	}

	if !isPcapOrGzip(outputFile) {
		t.Error("output file does not appear to be a valid pcap or gzip file")
	}

	t.Logf("capture output file created: %s", outputFile)
}

func TestCaptureFromDeployment(t *testing.T) {
	tmpDir := t.TempDir()
	outputTemplate := filepath.Join(tmpDir, "capture-deploy.pcap.gz")

	// Generate traffic concurrently
	go generateTraffic(t, "http://nginx-e2e.kpr-e2e-test.svc.cluster.local", 15)

	stdout, stderr, err := runBinary(t, 90*time.Second,
		"capture", "tcp port 80", "deployment", "nginx-e2e",
		"-n", testNamespace,
		"-d", "10s",
		"-o", outputTemplate,
	)
	if err != nil {
		t.Fatalf("capture from deployment failed: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
	}

	// The tool creates one file per pod when capturing from a deployment
	matches, _ := filepath.Glob(filepath.Join(tmpDir, "*.pcap*"))
	if len(matches) == 0 {
		t.Fatal("no capture files created for deployment")
	}

	t.Logf("created %d capture file(s) for deployment", len(matches))
	for _, m := range matches {
		if !fileExists(m) {
			t.Errorf("capture file %s is empty", m)
		}
	}
}

func TestCaptureDurationFlag(t *testing.T) {
	podName := getPodName(t, "app=nginx-e2e")
	tmpDir := t.TempDir()
	outputFile := filepath.Join(tmpDir, "duration-test.pcap.gz")

	start := time.Now()

	go generateTraffic(t, "http://nginx-e2e.kpr-e2e-test.svc.cluster.local", 5)

	stdout, stderr, err := runBinary(t, 45*time.Second,
		"capture", "tcp", "pod", podName,
		"-n", testNamespace,
		"-d", "5s",
		"-o", outputFile,
	)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("capture with duration failed: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
	}

	// Should complete in roughly 5s + overhead, not 30s+
	if elapsed > 30*time.Second {
		t.Errorf("capture took too long: %v (expected ~5s + overhead)", elapsed)
	}

	if !fileExists(outputFile) {
		t.Fatal("capture output file was not created")
	}

	t.Logf("capture with duration completed in %v", elapsed)
}
