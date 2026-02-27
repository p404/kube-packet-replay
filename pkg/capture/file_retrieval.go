package capture

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/p404/kube-packet-replay/pkg/k8s"
	outpkg "github.com/p404/kube-packet-replay/pkg/output"
)

// WaitForCompressedFile waits for the compressed file to be available with progress tracking
func WaitForCompressedFile(client *k8s.Client, namespace, podName, containerName,
	rawCaptureFile, compressedFilename string, verbose bool) (output string, fileSuccess bool, rawSize, compressedSize int) {

	// First, try to get the raw file size for comparison later
	rawOutput, err := tryExecCat(client, namespace, podName, containerName, rawCaptureFile, false)
	out := outpkg.Default()
	if err == nil {
		rawSize = len(rawOutput)
		if verbose {
			out.Print("  Raw capture file found: %d bytes\n", rawSize)
		}
	}

	// Try to get compressed file with a smart wait mechanism
	// Poll for the compressed file instead of using a fixed sleep
	maxAttempts := 15
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if verbose {
			out.Print("  Checking for compressed file (attempt %d/%d)...\n", attempt, maxAttempts)
		}

		// Try to get the compressed file
		output, err = tryExecCat(client, namespace, podName, containerName, compressedFilename, false)
		if err == nil {
			// File found!
			compressedSize = len(output)
			fileSuccess = true
			if verbose {
				out.Print("  Successfully retrieved compressed capture file (%d bytes)\n", compressedSize)
			}

			// Show compression statistics if we have raw size
			if rawSize > 0 && verbose {
				compressionRatio := float64(compressedSize) / float64(rawSize) * 100
				spaceReduction := 100.0 - compressionRatio
				bytesSaved := rawSize - compressedSize
				out.Print("  %s %d bytes -> %d bytes (%.1f%% smaller, saved %d bytes)\n",
					outpkg.Colorize(outpkg.ColorGreen, "Compression:"),
					rawSize, compressedSize, spaceReduction, bytesSaved)
			}
			break
		}

		// Check if gzip is running
		if checkGzipRunning(client, namespace, podName, containerName) {
			if verbose {
				out.Println("  Compression in progress... waiting...")
			}
		} else if attempt == maxAttempts/2 && verbose {
			// Check container logs halfway through to see what's happening
			out.Println("  Checking container logs...")
			logs := getContainerLogs(client, namespace, podName, containerName, 10)
			if logs != "" {
				out.Print("  Container log snippets:\n%s\n", logs)
			}
		}

		// Wait before next attempt
		time.Sleep(2 * time.Second)
	}

	// If we couldn't get compressed file but have raw, use that
	if !fileSuccess && rawSize > 0 {
		output = rawOutput
		fileSuccess = true
		if verbose {
			out.Print("  Using previously retrieved raw capture data (%d bytes)\n", rawSize)
			out.Warning("  Container compression failed or didn't complete")
		}
	}

	return output, fileSuccess, rawSize, compressedSize
}

// SaveCaptureToFile saves the captured data to a file with proper error handling
func SaveCaptureToFile(outputFile, data string, verbose bool) error {
	out := outpkg.Default()
	// Ensure directory exists
	dir := filepath.Dir(outputFile)
	if dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil && verbose {
			out.Warning("  Could not create directory %s: %v", dir, err)
		}
	}

	// Write the file
	err := os.WriteFile(outputFile, []byte(data), 0o600)
	if err == nil {
		fileInfo, statErr := os.Stat(outputFile)
		if statErr == nil && verbose {
			out.Print("  Packet capture written successfully (%d bytes)\n", fileInfo.Size())
		}
		return nil
	}

	// If saving failed, try with absolute path
	if verbose {
		out.Warning("  Failed to save to %s: %v", outputFile, err)
	}

	absPath, absErr := filepath.Abs(outputFile)
	if absErr == nil && absPath != outputFile {
		if verbose {
			out.Print("  Trying with absolute path: %s\n", absPath)
		}
		writeErr := os.WriteFile(absPath, []byte(data), 0o600)
		if writeErr == nil {
			if verbose {
				out.Print("  Packet capture saved to %s\n", absPath)
			}
			return nil
		}
	}

	// Try current directory as last resort
	fallbackFile := filepath.Join(".", fmt.Sprintf("captured-%d.pcap", time.Now().Unix()))
	if verbose {
		out.Print("  Trying to save to current directory: %s\n", fallbackFile)
	}
	writeErr := os.WriteFile(fallbackFile, []byte(data), 0o600)
	if writeErr == nil {
		if verbose {
			out.Print("  Packet capture saved to %s\n", fallbackFile)
		}
		return nil
	}

	return fmt.Errorf("failed to save capture file to any location: %v", err)
}

// formatBytes converts bytes to a human-readable format
func formatBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}

// formatBytesMulti is an alias for multi-capture use (accepts int)
func formatBytesMulti(b int) string {
	return formatBytes(int64(b))
}

// formatDurationHMS formats a duration as HH:MM:SS
func formatDurationHMS(d time.Duration) string {
	hours := int(d.Hours())
	minutes := int(d.Minutes()) % 60
	seconds := int(d.Seconds()) % 60
	return fmt.Sprintf("%02d:%02d:%02d", hours, minutes, seconds)
}
