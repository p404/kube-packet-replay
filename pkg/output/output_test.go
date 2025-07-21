package output

import (
	"bytes"
	"strings"
	"testing"
)

func TestConsoleWriter(t *testing.T) {
	// Create a buffer to capture output
	var buf bytes.Buffer
	
	// Create a console writer with the buffer
	writer := NewConsoleWriter()
	writer.SetWriter(&buf)
	
	// Test basic output methods
	writer.Info("Test info message")
	output := buf.String()
	if !strings.Contains(output, "Test info message") {
		t.Errorf("Expected output to contain 'Test info message', got: %s", output)
	}
	
	// Reset buffer
	buf.Reset()
	
	// Test warning with color
	writer.Warning("Test warning")
	output = buf.String()
	if !strings.Contains(output, "Test warning") {
		t.Errorf("Expected output to contain 'Test warning', got: %s", output)
	}
	if !strings.Contains(output, ColorYellow) {
		t.Errorf("Expected warning to contain yellow color code")
	}
	
	// Reset buffer
	buf.Reset()
	
	// Test error with color
	writer.Error("Test error")
	output = buf.String()
	if !strings.Contains(output, "Test error") {
		t.Errorf("Expected output to contain 'Test error', got: %s", output)
	}
	if !strings.Contains(output, ColorRed) {
		t.Errorf("Expected error to contain red color code")
	}
	
	// Reset buffer
	buf.Reset()
	
	// Test success with color
	writer.Success("Test success")
	output = buf.String()
	if !strings.Contains(output, "Test success") {
		t.Errorf("Expected output to contain 'Test success', got: %s", output)
	}
	if !strings.Contains(output, ColorGreen) {
		t.Errorf("Expected success to contain green color code")
	}
}

func TestColorization(t *testing.T) {
	// Test Colorize function
	result := Colorize(ColorBlue, "Blue text")
	expected := ColorBlue + "Blue text" + ColorReset
	if result != expected {
		t.Errorf("Expected '%s', got '%s'", expected, result)
	}
	
	// Test FormatBold function
	result = FormatBold("Bold text")
	expected = ColorBold + "Bold text" + ColorReset
	if result != expected {
		t.Errorf("Expected '%s', got '%s'", expected, result)
	}
	
	// Test FormatHighlight function
	result = FormatHighlight("Highlighted text")
	if !strings.Contains(result, "Highlighted text") {
		t.Errorf("Expected highlighted text to contain 'Highlighted text', got: %s", result)
	}
}

func TestOutputLevels(t *testing.T) {
	var buf bytes.Buffer
	writer := NewConsoleWriter()
	writer.SetWriter(&buf)
	
	// Set level to Error - only errors should be shown
	writer.SetLevel(LevelError)
	
	writer.Debug("Debug message")
	writer.Info("Info message")
	writer.Warning("Warning message")
	writer.Error("Error message")
	
	output := buf.String()
	
	// Should not contain debug, info, or warning
	if strings.Contains(output, "Debug message") {
		t.Errorf("Debug message should not be shown at Error level")
	}
	if strings.Contains(output, "Info message") {
		t.Errorf("Info message should not be shown at Error level")
	}
	if strings.Contains(output, "Warning message") {
		t.Errorf("Warning message should not be shown at Error level")
	}
	// Should contain error
	if !strings.Contains(output, "Error message") {
		t.Errorf("Error message should be shown at Error level")
	}
}