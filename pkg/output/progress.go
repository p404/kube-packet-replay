package output

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

// ProgressBar represents a progress bar
type ProgressBar struct {
	writer      *ConsoleWriter
	total       int
	current     int
	width       int
	message     string
	startTime   time.Time
	mu          sync.Mutex
	lastRender  string
	active      bool
}

// NewProgressBar creates a new progress bar
func (w *ConsoleWriter) NewProgressBar(total int, message string) *ProgressBar {
	return &ProgressBar{
		writer:    w,
		total:     total,
		current:   0,
		width:     40,
		message:   message,
		startTime: time.Now(),
		active:    true,
	}
}

// Update updates the progress bar
func (p *ProgressBar) Update(current int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	
	if !p.active {
		return
	}
	
	p.current = current
	p.render()
}

// UpdateMessage updates the progress bar message
func (p *ProgressBar) UpdateMessage(message string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	
	p.message = message
	if p.active {
		p.render()
	}
}

// Finish completes the progress bar
func (p *ProgressBar) Finish() {
	p.mu.Lock()
	defer p.mu.Unlock()
	
	if !p.active {
		return
	}
	
	p.current = p.total
	p.render()
	fmt.Fprintln(p.writer.writer) // New line after completion
	p.active = false
}

// Clear clears the progress bar
func (p *ProgressBar) Clear() {
	p.mu.Lock()
	defer p.mu.Unlock()
	
	if !p.active {
		return
	}
	
	// Clear the line
	clearLine := "\r" + strings.Repeat(" ", len(p.lastRender)) + "\r"
	fmt.Fprint(p.writer.writer, clearLine)
	p.active = false
}

// render draws the progress bar
func (p *ProgressBar) render() {
	if p.total <= 0 {
		return
	}
	
	percentage := float64(p.current) / float64(p.total)
	filled := int(percentage * float64(p.width))
	
	bar := strings.Repeat("█", filled) + strings.Repeat("░", p.width-filled)
	
	elapsed := time.Since(p.startTime)
	var eta string
	if p.current > 0 && p.current < p.total {
		remaining := float64(p.total-p.current) / float64(p.current) * elapsed.Seconds()
		eta = fmt.Sprintf(" ETA: %s", formatDuration(time.Duration(remaining*float64(time.Second))))
	}
	
	output := fmt.Sprintf("\r%s [%s] %d/%d (%.1f%%)%s",
		p.message,
		p.writer.colorize(ColorGreen, bar),
		p.current,
		p.total,
		percentage*100,
		eta,
	)
	
	// Pad with spaces to clear any previous longer output
	if len(output) < len(p.lastRender) {
		output += strings.Repeat(" ", len(p.lastRender)-len(output))
	}
	
	fmt.Fprint(p.writer.writer, output)
	p.lastRender = output
}

// formatDuration formats a duration in a human-readable way
func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm%ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	return fmt.Sprintf("%dh%dm", int(d.Hours()), int(d.Minutes())%60)
}

// MultiProgress manages multiple progress items
type MultiProgress struct {
	writer    *ConsoleWriter
	items     map[string]*ProgressItem
	mu        sync.Mutex
	active    bool
	renderMu  sync.Mutex
}

// ProgressItem represents a single item in multi-progress display
type ProgressItem struct {
	name     string
	status   string
	progress int
	total    int
	done     bool
	error    error
}

// NewMultiProgress creates a new multi-progress display
func (w *ConsoleWriter) NewMultiProgress() *MultiProgress {
	return &MultiProgress{
		writer: w,
		items:  make(map[string]*ProgressItem),
		active: true,
	}
}

// AddItem adds a new item to track
func (m *MultiProgress) AddItem(name string, total int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	
	m.items[name] = &ProgressItem{
		name:   name,
		status: "Pending",
		total:  total,
	}
}

// UpdateItem updates an item's progress
func (m *MultiProgress) UpdateItem(name string, progress int, status string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	
	if item, exists := m.items[name]; exists {
		item.progress = progress
		item.status = status
		m.render()
	}
}

// CompleteItem marks an item as complete
func (m *MultiProgress) CompleteItem(name string, success bool, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	
	if item, exists := m.items[name]; exists {
		item.done = true
		item.error = err
		if success {
			item.status = "Completed"
			item.progress = item.total
		} else {
			item.status = "Failed"
		}
		m.render()
	}
}

// render displays all progress items
func (m *MultiProgress) render() {
	m.renderMu.Lock()
	defer m.renderMu.Unlock()
	
	if !m.active {
		return
	}
	
	// Clear previous output
	numLines := len(m.items) + 1
	for i := 0; i < numLines; i++ {
		fmt.Fprintf(m.writer.writer, "\033[A\033[K")
	}
	
	// Render each item
	for _, item := range m.items {
		var symbol string
		var color string
		
		if item.done {
			if item.error != nil {
				symbol = "✗"
				color = ColorRed
			} else {
				symbol = "✓"
				color = ColorGreen
			}
		} else {
			symbol = "•"
			color = ColorBlue
		}
		
		status := item.status
		if item.total > 0 && !item.done {
			percentage := float64(item.progress) / float64(item.total) * 100
			status = fmt.Sprintf("%s (%.0f%%)", status, percentage)
		}
		
		fmt.Fprintf(m.writer.writer, "  %s %s: %s\n",
			m.writer.colorize(color, symbol),
			item.name,
			status,
		)
	}
}

// Finish completes the multi-progress display
func (m *MultiProgress) Finish() {
	m.mu.Lock()
	defer m.mu.Unlock()
	
	m.active = false
	m.render()
}