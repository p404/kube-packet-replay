package output

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"
)

// Level represents the severity level of output
type Level int

const (
	LevelDebug Level = iota
	LevelInfo
	LevelWarning
	LevelError
	LevelNone
)

// Writer handles all console output
type Writer interface {
	Debug(format string, args ...interface{})
	Info(format string, args ...interface{})
	Warning(format string, args ...interface{})
	Error(format string, args ...interface{})
	Success(format string, args ...interface{})

	Step(number int, message string)
	Print(format string, args ...interface{})
	Println(args ...interface{})

	StartSpinner(message string) *Spinner
	StopSpinner(spinner *Spinner)

	SetLevel(level Level)
	SetWriter(w io.Writer)

	// Formatting methods
	Colorize(color, text string) string
	FormatBold(text string) string
	FormatHighlight(text string) string
}

// ConsoleWriter implements Writer for terminal output
type ConsoleWriter struct {
	writer   io.Writer
	spinners map[*Spinner]bool
	mu       sync.Mutex
	level    Level
	noColor  bool
}

// NewConsoleWriter creates a new console writer
func NewConsoleWriter() *ConsoleWriter {
	return &ConsoleWriter{
		writer:   os.Stdout,
		level:    LevelInfo,
		noColor:  os.Getenv("NO_COLOR") != "",
		spinners: make(map[*Spinner]bool),
	}
}

// SetLevel sets the minimum output level
func (w *ConsoleWriter) SetLevel(level Level) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.level = level
}

// SetWriter sets the output writer
func (w *ConsoleWriter) SetWriter(writer io.Writer) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.writer = writer
}

// Debug outputs debug information
func (w *ConsoleWriter) Debug(format string, args ...interface{}) {
	if w.level <= LevelDebug {
		w.write(LevelDebug, format, args...)
	}
}

// Info outputs informational messages
func (w *ConsoleWriter) Info(format string, args ...interface{}) {
	if w.level <= LevelInfo {
		w.write(LevelInfo, format, args...)
	}
}

// Warning outputs warning messages
func (w *ConsoleWriter) Warning(format string, args ...interface{}) {
	if w.level <= LevelWarning {
		w.write(LevelWarning, format, args...)
	}
}

// Error outputs error messages
func (w *ConsoleWriter) Error(format string, args ...interface{}) {
	if w.level <= LevelError {
		w.write(LevelError, format, args...)
	}
}

// Success outputs success messages
func (w *ConsoleWriter) Success(format string, args ...interface{}) {
	if w.level <= LevelInfo {
		prefix := w.colorize(ColorGreen, "✓")
		w.writeWithPrefix(prefix, format, args...)
	}
}

// Step outputs a step indicator with step number
func (w *ConsoleWriter) Step(number int, message string) {
	if w.level <= LevelInfo {
		prefix := w.colorize(ColorCyan, fmt.Sprintf("→ Step %d:", number))
		w.writeWithPrefix(prefix, "%s", message)
	}
}

// Print outputs without any formatting
func (w *ConsoleWriter) Print(format string, args ...interface{}) {
	w.mu.Lock()
	defer w.mu.Unlock()
	fmt.Fprintf(w.writer, format, args...)
}

// Println outputs a line without any formatting
func (w *ConsoleWriter) Println(args ...interface{}) {
	w.mu.Lock()
	defer w.mu.Unlock()
	fmt.Fprintln(w.writer, args...)
}

// write handles the actual writing with appropriate formatting
func (w *ConsoleWriter) write(level Level, format string, args ...interface{}) {
	var prefix string
	switch level {
	case LevelDebug:
		prefix = w.colorize(ColorGray, "[DEBUG]")
	case LevelInfo:
		prefix = w.colorize(ColorBlue, "ℹ")
	case LevelWarning:
		prefix = w.colorize(ColorYellow, "⚠")
	case LevelError:
		prefix = w.colorize(ColorRed, "✗")
	}

	w.writeWithPrefix(prefix, format, args...)
}

// writeWithPrefix writes with a given prefix
func (w *ConsoleWriter) writeWithPrefix(prefix, format string, args ...interface{}) {
	w.mu.Lock()
	defer w.mu.Unlock()

	message := fmt.Sprintf(format, args...)
	if prefix != "" {
		fmt.Fprintf(w.writer, "%s %s\n", prefix, message)
	} else {
		fmt.Fprintf(w.writer, "%s\n", message)
	}
}

// colorize applies color to text if colors are enabled
func (w *ConsoleWriter) colorize(color, text string) string {
	if w.noColor {
		return text
	}
	return color + text + ColorReset
}

// Colorize applies color to a string (public method for external use)
func (w *ConsoleWriter) Colorize(color, text string) string {
	return w.colorize(color, text)
}

// FormatBold makes text bold
func (w *ConsoleWriter) FormatBold(text string) string {
	return w.colorize(ColorBold, text)
}

// FormatHighlight highlights text with bold yellow
func (w *ConsoleWriter) FormatHighlight(text string) string {
	return w.colorize(ColorBold+ColorYellow, text)
}

// StartSpinner starts a new spinner
func (w *ConsoleWriter) StartSpinner(message string) *Spinner {
	spinner := &Spinner{
		message: message,
		writer:  w,
		done:    make(chan bool),
		active:  true,
	}

	w.mu.Lock()
	w.spinners[spinner] = true
	w.mu.Unlock()

	go spinner.run()
	return spinner
}

// StopSpinner stops a spinner
func (w *ConsoleWriter) StopSpinner(spinner *Spinner) {
	if spinner == nil || !spinner.active {
		return
	}

	w.mu.Lock()
	delete(w.spinners, spinner)
	w.mu.Unlock()

	spinner.Stop()
}

// StopAllSpinners stops all active spinners
func (w *ConsoleWriter) StopAllSpinners() {
	w.mu.Lock()
	spinners := make([]*Spinner, 0, len(w.spinners))
	for spinner := range w.spinners {
		spinners = append(spinners, spinner)
	}
	w.mu.Unlock()

	for _, spinner := range spinners {
		w.StopSpinner(spinner)
	}
}

// Spinner represents an animated spinner
type Spinner struct {
	writer  *ConsoleWriter
	done    chan bool
	message string
	mu      sync.Mutex
	active  bool
}

// run runs the spinner animation
func (s *Spinner) run() {
	chars := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
	i := 0

	for {
		select {
		case <-s.done:
			// Clear the spinner line
			fmt.Fprintf(s.writer.writer, "\r%s\r", strings.Repeat(" ", len(s.message)+10))
			return
		default:
			s.mu.Lock()
			if s.active {
				fmt.Fprintf(s.writer.writer, "\r%s %s",
					s.writer.colorize(ColorBlue, chars[i%len(chars)]),
					s.message)
			}
			s.mu.Unlock()

			i++
			time.Sleep(100 * time.Millisecond)
		}
	}
}

// Stop stops the spinner
func (s *Spinner) Stop() {
	s.mu.Lock()
	if s.active {
		s.active = false
		close(s.done)
	}
	s.mu.Unlock()
}

// Update updates the spinner message
func (s *Spinner) Update(message string) {
	s.mu.Lock()
	s.message = message
	s.mu.Unlock()
}

// Default writer instance
var defaultWriter = NewConsoleWriter()

// Default returns the default writer instance
func Default() Writer {
	return defaultWriter
}

// Helper functions that use the default writer
func Debug(format string, args ...interface{}) {
	defaultWriter.Debug(format, args...)
}

func Info(format string, args ...interface{}) {
	defaultWriter.Info(format, args...)
}

func Warning(format string, args ...interface{}) {
	defaultWriter.Warning(format, args...)
}

func Error(format string, args ...interface{}) {
	defaultWriter.Error(format, args...)
}

func Success(format string, args ...interface{}) {
	defaultWriter.Success(format, args...)
}

func Step(number int, message string) {
	defaultWriter.Step(number, message)
}

func Print(format string, args ...interface{}) {
	defaultWriter.Print(format, args...)
}

func Println(args ...interface{}) {
	defaultWriter.Println(args...)
}

// Helper formatting functions
func Colorize(color, text string) string {
	return defaultWriter.colorize(color, text)
}

func FormatBold(text string) string {
	return defaultWriter.FormatBold(text)
}

func FormatHighlight(text string) string {
	return defaultWriter.FormatHighlight(text)
}
