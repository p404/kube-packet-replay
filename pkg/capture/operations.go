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
                         rawCaptureFile, compressedFilename string, verbose bool) (string, bool, int, int) {
	
	// First, try to get the raw file size for comparison later
	rawOutput, err := tryExecCat(client, namespace, podName, containerName, rawCaptureFile, false)
	var rawSize int
	out := outpkg.Default()
	if err == nil {
		rawSize = len(rawOutput)
		if verbose {
			out.Print("  Raw capture file found: %d bytes\n", rawSize)
		}
	}
	
	// Try to get compressed file with a smart wait mechanism
	var output string
	var fileSuccess bool
	var compressedSize int
	
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
				out.Print("  %s %d bytes → %d bytes (%.1f%% smaller, saved %d bytes)\n",
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
	
	// Progress completed

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
func SaveCaptureToFile(outputFile string, data string, verbose bool) error {
	out := outpkg.Default()
	// Ensure directory exists
	dir := filepath.Dir(outputFile)
	if dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0755); err != nil && verbose {
			out.Warning("  Could not create directory %s: %v", dir, err)
		}
	}

	// Write the file
	err := os.WriteFile(outputFile, []byte(data), 0644)
	if err == nil {
		fileInfo, err := os.Stat(outputFile)
		if err == nil && verbose {
			out.Print("  Packet capture written successfully (%d bytes)\n", fileInfo.Size())
		}
		return nil
	}

	// If saving failed, try fallbacks
	if verbose {
		out.Warning("  Failed to save to %s: %v", outputFile, err)
	}
	
	// Try absolute path
	absPath, err2 := filepath.Abs(outputFile)
	if err2 == nil && absPath != outputFile {
		if verbose {
			out.Print("  Trying with absolute path: %s\n", absPath)
		}
		err = os.WriteFile(absPath, []byte(data), 0644)
		if err == nil {
			if verbose {
				out.Print("  Packet capture saved to %s\n", absPath)
			}
			return nil
		}
	}

	// Try home directory
	homeDir, err := os.UserHomeDir()
	if err == nil {
		homeFile := filepath.Join(homeDir, "captured.pcap")
		if verbose {
			out.Print("  Trying to save to home directory: %s\n", homeFile)
		}
		err = os.WriteFile(homeFile, []byte(data), 0644)
		if err == nil {
			if verbose {
				out.Print("  Packet capture saved to %s\n", homeFile)
			}
			return nil
		}
	}

	// Last resort, try current directory
	currentDir := "./captured.pcap"
	if verbose {
		out.Print("  Trying to save to current directory: %s\n", currentDir)
	}
	err = os.WriteFile(currentDir, []byte(data), 0644)
	if err == nil {
		if verbose {
			out.Print("  Packet capture saved to %s\n", currentDir)
		}
		return nil
	}

	return fmt.Errorf("failed to save capture file to any location: %v", err)
}
