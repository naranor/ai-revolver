// Package logger provides structured logging with an asynchronous ring buffer
package logger

import (
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.com/rs/zerolog"
)

var (
	log         zerolog.Logger
	logChan     chan logEntry
	wg          sync.WaitGroup
	initialized bool
	mu          sync.Mutex
)

type logEntry struct {
	fields map[string]interface{}
	msg    string
	level  zerolog.Level
}

// Init initializes the logging system
func Init(debug bool) {
	level := zerolog.InfoLevel
	var output io.Writer

	if debug {
		level = zerolog.DebugLevel
		output = zerolog.ConsoleWriter{
			Out:        os.Stderr,
			TimeFormat: "15:04:05",
		}
	} else {
		output = os.Stderr
	}

	zerolog.TimeFieldFormat = time.RFC3339
	zerolog.SetGlobalLevel(level)

	log = zerolog.New(output).With().Timestamp().Logger()

	// Initialize ring buffer (fixed size 100)
	GlobalRingBuffer = NewRingBuffer(100)

	// Initialize async logging channel
	logChan = make(chan logEntry, 1000) // Buffer for 1000 entries
	initialized = true

	// Start async writer
	wg.Add(1)
	go asyncWriter(logChan)
}

func asyncWriter(ch chan logEntry) {
	defer wg.Done()
	for entry := range ch {
		// Store in ring buffer
		GlobalRingBuffer.Add(entry.level, entry.msg, entry.fields)

		event := log.WithLevel(entry.level)
		for k, v := range entry.fields {
			event = event.Interface(k, v)
		}
		event.Msg(entry.msg)
	}
}

// Shutdown gracefully stops the logger, flushing remaining entries.
// It is safe to call Shutdown multiple times.
func Shutdown() {
	mu.Lock()
	if !initialized {
		mu.Unlock()
		return
	}
	initialized = false
	ch := logChan
	logChan = nil
	mu.Unlock()
	close(ch)
	wg.Wait()
}

func send(level zerolog.Level, msg string, fields map[string]interface{}) {
	mu.Lock()
	if !initialized {
		mu.Unlock()
		return
	}
	ch := logChan
	mu.Unlock()

	select {
	case ch <- logEntry{level: level, msg: msg, fields: fields}:
	default:
		// Channel full, drop to prevent blocking
	}
}

// Debug logs a debug message
func Debug() *Event {
	return &Event{level: zerolog.DebugLevel, fields: make(map[string]interface{})}
}

// Info logs an info message
func Info() *Event {
	return &Event{level: zerolog.InfoLevel, fields: make(map[string]interface{})}
}

// Warn logs a warning message
func Warn() *Event {
	return &Event{level: zerolog.WarnLevel, fields: make(map[string]interface{})}
}

// Error logs an error message
func Error() *Event {
	return &Event{level: zerolog.ErrorLevel, fields: make(map[string]interface{})}
}

// Fatal logs a fatal message and exits (synchronous)
func Fatal() *Event {
	return &Event{level: zerolog.FatalLevel, fields: make(map[string]interface{})}
}

// Event wraps zerolog event for async logging
type Event struct {
	fields map[string]interface{}
	level  zerolog.Level
}

func (e *Event) ensureFields() {
	if e.fields == nil {
		e.fields = make(map[string]interface{}, 4)
	}
}

// Err adds an error field to the event
func (e *Event) Err(err error) *Event {
	e.ensureFields()
	e.fields["error"] = err.Error()
	return e
}

// Str adds a string field to the event
func (e *Event) Str(key, val string) *Event {
	e.ensureFields()
	e.fields[key] = val
	return e
}

// Int adds an integer field to the event
func (e *Event) Int(key string, val int) *Event {
	e.ensureFields()
	e.fields[key] = val
	return e
}

// Int64 adds an int64 field to the event
func (e *Event) Int64(key string, val int64) *Event {
	e.ensureFields()
	e.fields[key] = val
	return e
}

// Bool adds a boolean field to the event
func (e *Event) Bool(key string, val bool) *Event {
	e.ensureFields()
	e.fields[key] = val
	return e
}

// Float64 adds a float64 field to the event
func (e *Event) Float64(key string, val float64) *Event {
	e.ensureFields()
	e.fields[key] = val
	return e
}

// Dur adds a duration field to the event
func (e *Event) Dur(key string, val time.Duration) *Event {
	e.ensureFields()
	e.fields[key] = val
	return e
}

// Interface adds an interface field to the event
func (e *Event) Interface(key string, val interface{}) *Event {
	e.ensureFields()
	e.fields[key] = val
	return e
}

// Msg logs the event with a message
func (e *Event) Msg(msg string) {
	fields := e.fields
	if fields == nil {
		fields = make(map[string]interface{})
	}
	send(e.level, msg, fields)
}

// Msgf logs the event with a formatted message
func (e *Event) Msgf(format string, args ...interface{}) {
	e.Msg(fmt.Sprintf(format, args...))
}
