package cli

// ANSI color codes for terminal output
const (
	ColorReset  = "\033[0m"
	ColorRed    = "\033[31m"
	ColorGreen  = "\033[32m"
	ColorYellow = "\033[33m"
	ColorBlue   = "\033[34m"
	ColorPurple = "\033[35m"
	ColorCyan   = "\033[36m"
	ColorWhite  = "\033[37m"
	ColorBold   = "\033[1m"
)

// Colorize applies color to a string
func Colorize(color, text string) string {
	return color + text + ColorReset
}

// Success formats a success message in green
func Success(message string) string {
	return Colorize(ColorGreen, "✓ "+message)
}

// Info formats an info message in blue
func Info(message string) string {
	return Colorize(ColorBlue, "ℹ "+message)
}

// Warning formats a warning message in yellow
func Warning(message string) string {
	return Colorize(ColorYellow, "⚠ "+message)
}

// Error formats an error message in red
func Error(message string) string {
	return Colorize(ColorRed, "✗ "+message)
}

// Step formats a step indicator in cyan
func Step(number int, message string) string {
	return Colorize(ColorCyan, "→ "+message)
}

// LoadingSpinner returns a spinner character for a given iteration
func LoadingSpinner(iteration int) string {
	chars := []string{"|", "/", "-", "\\"}
	return chars[iteration%len(chars)]
}
