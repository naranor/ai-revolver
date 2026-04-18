package logger

import (
	"sync"
	"time"

	"github.com/rs/zerolog"
)

// LogEntry represents a single log record in the ring buffer
type LogEntry struct {
	Timestamp time.Time              `json:"timestamp"`
	Fields    map[string]interface{} `json:"fields,omitempty"`
	Message   string                 `json:"message"`
	Level     zerolog.Level          `json:"level"`
}

// RingBuffer is a thread-safe fixed-size circular buffer for logs
type RingBuffer struct {
	entries []LogEntry
	size    int
	head    int
	mu      sync.RWMutex
	full    bool
}

// GlobalRingBuffer is the global ring buffer instance for logs
var GlobalRingBuffer *RingBuffer

// NewRingBuffer creates a new RingBuffer with the specified size
func NewRingBuffer(size int) *RingBuffer {
	return &RingBuffer{
		entries: make([]LogEntry, size),
		size:    size,
	}
}

// Add appends a new entry to the ring buffer
func (rb *RingBuffer) Add(level zerolog.Level, msg string, fields map[string]interface{}) {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	rb.entries[rb.head] = LogEntry{
		Timestamp: time.Now(),
		Level:     level,
		Message:   msg,
		Fields:    fields,
	}

	rb.head = (rb.head + 1) % rb.size
	if rb.head == 0 {
		rb.full = true
	}
}

// Get returns entries matching the level filter in chronological order
func (rb *RingBuffer) Get(levelFilter zerolog.Level) []LogEntry {
	rb.mu.RLock()
	defer rb.mu.RUnlock()

	var result []LogEntry
	start := 0
	count := rb.head

	if rb.full {
		start = rb.head
		count = rb.size
	}

	for i := 0; i < count; i++ {
		idx := (start + i) % rb.size
		entry := rb.entries[idx]
		// In zerolog, higher values are more severe. NoLevel is -1.
		if levelFilter == zerolog.NoLevel || entry.Level >= levelFilter {
			result = append(result, entry)
		}
	}

	return result
}

// Reset clears the ring buffer
func (rb *RingBuffer) Reset() {
	rb.mu.Lock()
	defer rb.mu.Unlock()
	rb.entries = make([]LogEntry, rb.size)
	rb.head = 0
	rb.full = false
}
