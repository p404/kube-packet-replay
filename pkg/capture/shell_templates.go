package capture

import (
	"fmt"
)

// ShellTemplates contains all shell script templates used in the capture process
const (
	// CleanupShellFunction is the shell function that handles graceful termination and cleanup
	CleanupShellFunction = `
# Define cleanup function for proper signal handling
cleanup() {
  signal_type=$1
  echo "[$(date)] Received signal $signal_type, stopping tcpdump gracefully..."
  
  # Stop tcpdump if it's running
  if [ -n "$TCPDUMP_PID" ] && ps -p $TCPDUMP_PID > /dev/null; then
    echo "[$(date)] Stopping tcpdump process (PID: $TCPDUMP_PID)..."
    kill -TERM $TCPDUMP_PID 2>/dev/null
    
    # Wait for tcpdump to exit (max 5 seconds)
    for i in $(seq 1 5); do
      if ! ps -p $TCPDUMP_PID > /dev/null; then
        echo "[$(date)] Tcpdump process exited"
        break
      fi
      echo "[$(date)] Waiting for tcpdump to exit ($i/5)..."
      sleep 1
    done
    
    # Force kill if needed
    if ps -p $TCPDUMP_PID > /dev/null; then
      echo "[$(date)] Force killing tcpdump..."
      kill -9 $TCPDUMP_PID 2>/dev/null
    fi
  fi
  
  # Wait a moment for file system to sync
  sync
  sleep 1
  
  # Check if raw capture file exists and compress it
  if [ -f "%s" ]; then
    filesize=$(stat -c %%s %s 2>/dev/null || ls -l %s | awk '{print $5}' 2>/dev/null)
    
    if [ -n "$filesize" ] && [ "$filesize" -gt 0 ]; then
      echo "[$(date)] Raw capture file created: $filesize bytes"
      echo "[$(date)] Compressing capture file with gzip..."
      
      # Use gzip with best compression
      gzip -9 -c %s > %s
      
      if [ -f "%s" ]; then
        compressed_size=$(stat -c %%s %s 2>/dev/null || ls -l %s | awk '{print $5}' 2>/dev/null)
        if [ -n "$compressed_size" ] && [ "$compressed_size" -gt 0 ]; then
          echo "[$(date)] Compression complete: $compressed_size bytes"
          echo "[$(date)] Compression ratio: $(echo "scale=2; (1-($compressed_size/$filesize))*100" | bc)%%"
          
          # Clean up raw file if compression was successful
          rm -f %s
        else
          echo "[$(date)] Warning: Compressed file is empty, keeping raw file"
        fi
      else
        echo "[$(date)] Warning: Compression failed, keeping raw file"
      fi
    else
      echo "[$(date)] Warning: Raw capture file is empty"
    fi
  else
    echo "[$(date)] Warning: No raw capture file found"
  fi
  
  echo "[$(date)] Keeping container alive for file retrieval (60 min timeout)"
  sleep 3600
}`

	// TcpdumpStartupTemplate is the shell script template for starting tcpdump
	TcpdumpStartupTemplate = `
# Set up signal traps
trap 'cleanup TERM' TERM
trap 'cleanup INT' INT

echo "[$(date)] Starting packet capture on interface %s..."
echo "[$(date)] Filter: '%s'"
echo "[$(date)] Writing to temporary file %s"
echo "[$(date)] Will compress to %s when done"

# Run tcpdump with proper buffering and output file
tcpdump -i %s -U -w %s '%s' &
TCPDUMP_PID=$!
echo "[$(date)] Tcpdump started with PID: $TCPDUMP_PID"

# Wait for tcpdump to exit or be killed
wait $TCPDUMP_PID || true
echo "[$(date)] Tcpdump process ended"

# Call cleanup to ensure compression happens
cleanup "EXIT"
`
)

// BuildTcpdumpCommand constructs the tcpdump shell command with proper templates
func BuildTcpdumpCommand(interfaceFlag, filterExpr, rawFile, compressedFile string) string {
	// Start with cleanup function
	script := fmt.Sprintf(CleanupShellFunction,
		rawFile, rawFile, rawFile,
		rawFile, compressedFile,
		compressedFile, compressedFile, compressedFile,
		rawFile)

	// Add the tcpdump startup
	script += fmt.Sprintf(TcpdumpStartupTemplate,
		interfaceFlag, filterExpr, rawFile, compressedFile,
		interfaceFlag, rawFile, filterExpr)

	return script
}
