package logger

import (
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
)

func TestRingBuffer(t *testing.T) {
	rb := NewRingBuffer(3)

	// Test adding entries
	rb.Add(zerolog.InfoLevel, "msg 1", nil)
	rb.Add(zerolog.WarnLevel, "msg 2", nil)
	rb.Add(zerolog.ErrorLevel, "msg 3", nil)

	entries := rb.Get(zerolog.NoLevel)
	assert.Equal(t, 3, len(entries))
	assert.Equal(t, "msg 1", entries[0].Message)
	assert.Equal(t, "msg 2", entries[1].Message)
	assert.Equal(t, "msg 3", entries[2].Message)

	// Test chronological order after overflow
	rb.Add(zerolog.InfoLevel, "msg 4", nil)
	entries = rb.Get(zerolog.NoLevel)
	assert.Equal(t, 3, len(entries))
	assert.Equal(t, "msg 2", entries[0].Message)
	assert.Equal(t, "msg 3", entries[1].Message)
	assert.Equal(t, "msg 4", entries[2].Message)

	// Test level filter
	warnEntries := rb.Get(zerolog.WarnLevel)
	assert.Equal(t, 2, len(warnEntries))
	assert.Equal(t, "msg 2", warnEntries[0].Message) // Warn
	assert.Equal(t, "msg 3", warnEntries[1].Message) // Error
}
