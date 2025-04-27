package capture

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/p404/kube-packet-replay/pkg/cli"
	"github.com/p404/kube-packet-replay/pkg/k8s"
)

// WaitForCompressedFile waits for the compressed file to be available with progress tracking
func WaitForCompressedFile(client *k8s.Client, namespace, podName, containerName, 
                         rawCaptureFile, compressedFilename string, verbose bool) (string, bool, int, int) {
	
	// First, try to get the raw file size for comparison later
	rawOutput, err := tryExecCat(client, namespace, podName, containerName, rawCaptureFile, false)
	var rawSize int
	if err == nil {
		rawSize = len(rawOutput)
		if verbose {
			fmt.Printf("  Raw capture file found: %d bytes\n", rawSize)
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
			fmt.Printf("  Checking for compressed file (attempt %d/%d)...\n", attempt, maxAttempts)
		} else {
			// Show a simple loading spinner in non-verbose mode
			fmt.Printf("\r  Processing capture %s ", cli.Colorize(cli.ColorBlue, cli.LoadingSpinner(attempt)))
		}
		
		// Try to get the compressed file
		output, err = tryExecCat(client, namespace, podName, containerName, compressedFilename, false)
		if err == nil {
			// File found!
			compressedSize = len(output)
			fileSuccess = true
			if verbose {
				fmt.Printf("  Successfully retrieved compressed capture file (%d bytes)\n", compressedSize)
			}
			
			// Show compression statistics if we have raw size
			if rawSize > 0 && verbose {
				compressionRatio := float64(compressedSize) / float64(rawSize) * 100
				spaceReduction := 100.0 - compressionRatio
				bytesSaved := rawSize - compressedSize
				fmt.Printf("  %s %d bytes → %d bytes (%.1f%% smaller, saved %d bytes)\n",
					cli.Colorize(cli.ColorGreen, "Compression:"), 
					rawSize, compressedSize, spaceReduction, bytesSaved)
			}
			break
		}
		
		// Check if gzip is running
		if checkGzipRunning(client, namespace, podName, containerName) {
			if verbose {
				fmt.Println("  Compression in progress... waiting...")
			}
		} else if attempt == maxAttempts/2 && verbose {
			// Check container logs halfway through to see what's happening
			fmt.Println("  Checking container logs...")
			logs := getContainerLogs(client, namespace, podName, containerName, 10)
			if logs != "" {
				fmt.Printf("  Container log snippets:\n%s\n", logs)
			}
		}
		
		// Wait before next attempt
		time.Sleep(2 * time.Second)
	}
	
	// Clear the progress line in non-verbose mode
	if !verbose {
		fmt.Print("\r  Waiting for container to process capture file...                           \n")
	}

	// If we couldn't get compressed file but have raw, use that
	if !fileSuccess && rawSize > 0 {
		output = rawOutput
		fileSuccess = true
		if verbose {
			fmt.Printf("  Using previously retrieved raw capture data (%d bytes)\n", rawSize)
			fmt.Printf("  %s Container compression failed or didn't complete\n", 
				cli.Colorize(cli.ColorYellow, "WARNING:"))
		}
	}
	
	return output, fileSuccess, rawSize, compressedSize
}

// SaveCaptureToFile saves the captured data to a file with proper error handling
func SaveCaptureToFile(outputFile string, data string, verbose bool) error {
	// Ensure directory exists
	dir := filepath.Dir(outputFile)
	if dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0755); err != nil && verbose {
			fmt.Printf("  %s Could not create directory %s: %v\n", 
				cli.Colorize(cli.ColorYellow, "Warning:"), dir, err)
		}
	}

	// Write the file
	err := os.WriteFile(outputFile, []byte(data), 0644)
	if err == nil {
		fileInfo, err := os.Stat(outputFile)
		if err == nil && verbose {
			fmt.Printf("  Packet capture written successfully (%d bytes)\n", fileInfo.Size())
		}
		return nil
	}

	// If saving failed, try fallbacks
	if verbose {
		fmt.Printf("  %s Failed to save to %s: %v\n", 
			cli.Colorize(cli.ColorYellow, "Warning:"), outputFile, err)
	}
	
	// Try absolute path
	absPath, err2 := filepath.Abs(outputFile)
	if err2 == nil && absPath != outputFile {
		if verbose {
			fmt.Printf("  Trying with absolute path: %s\n", absPath)
		}
		err = os.WriteFile(absPath, []byte(data), 0644)
		if err == nil {
			if verbose {
				fmt.Printf("  Packet capture saved to %s\n", absPath)
			}
			return nil
		}
	}

	// Try home directory
	homeDir, err := os.UserHomeDir()
	if err == nil {
		homeFile := filepath.Join(homeDir, "captured.pcap")
		if verbose {
			fmt.Printf("  Trying to save to home directory: %s\n", homeFile)
		}
		err = os.WriteFile(homeFile, []byte(data), 0644)
		if err == nil {
			if verbose {
				fmt.Printf("  Packet capture saved to %s\n", homeFile)
			}
			return nil
		}
	}

	// Last resort, try current directory
	currentDir := "./captured.pcap"
	if verbose {
		fmt.Printf("  Trying to save to current directory: %s\n", currentDir)
	}
	err = os.WriteFile(currentDir, []byte(data), 0644)
	if err == nil {
		if verbose {
			fmt.Printf("  Packet capture saved to %s\n", currentDir)
		}
		return nil
	}

	return fmt.Errorf("failed to save capture file to any location: %v", err)
}
