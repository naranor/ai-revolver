package logger

import (
	"errors"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// resetLogger tears down any running logger state so tests are isolated.
func resetLogger() {
	if initialized {
		Shutdown()
		initialized = false
	}
}

func TestInit_NonDebug(t *testing.T) {
	resetLogger()
	Init(false)
	defer resetLogger()

	assert.True(t, initialized)
	assert.NotNil(t, GlobalRingBuffer)
	assert.NotNil(t, logChan)
}

func TestInit_Debug(t *testing.T) {
	resetLogger()
	Init(true)
	defer resetLogger()

	assert.True(t, initialized)
	assert.NotNil(t, GlobalRingBuffer)
}

func TestShutdown_NotInitialized(t *testing.T) {
	resetLogger()
	// Should be safe to call when not initialized
	Shutdown()
}

func TestShutdown_Initialized(t *testing.T) {
	resetLogger()
	Init(false)
	Shutdown()
	initialized = false // mark as not initialized after shutdown so resetLogger is safe
}

func TestSend_NotInitialized(t *testing.T) {
	resetLogger()
	// Should be a no-op
	send(zerolog.InfoLevel, "test", nil)
}

func TestSend_ChannelFull(t *testing.T) {
	resetLogger()
	Init(false)
	defer resetLogger()

	// Fill the channel; extra sends should be silently dropped
	for i := 0; i < 1200; i++ {
		send(zerolog.InfoLevel, "flood", nil)
	}
}

func TestEventLevels(t *testing.T) {
	resetLogger()
	Init(false)
	defer resetLogger()

	// All of these should return a non-nil Event without panicking
	assert.NotNil(t, Debug())
	assert.NotNil(t, Info())
	assert.NotNil(t, Warn())
	assert.NotNil(t, Error())
	assert.NotNil(t, Fatal())
}

func TestEventFields(t *testing.T) {
	resetLogger()
	Init(false)
	defer resetLogger()

	e := Info().
		Str("str_key", "str_val").
		Int("int_key", 42).
		Int64("int64_key", int64(100)).
		Bool("bool_key", true).
		Float64("float_key", 3.14).
		Dur("dur_key", time.Second).
		Interface("iface_key", []string{"a", "b"}).
		Err(errors.New("test error"))

	require.NotNil(t, e)
	assert.Equal(t, "str_val", e.fields["str_key"])
	assert.Equal(t, 42, e.fields["int_key"])
	assert.Equal(t, int64(100), e.fields["int64_key"])
	assert.Equal(t, true, e.fields["bool_key"])
	assert.InDelta(t, 3.14, e.fields["float_key"], 0.001)
	assert.Equal(t, time.Second, e.fields["dur_key"])
	assert.Equal(t, []string{"a", "b"}, e.fields["iface_key"])
	assert.Equal(t, "test error", e.fields["error"])
}

func TestEventMsg(t *testing.T) {
	resetLogger()
	Init(false)
	defer resetLogger()

	// Msg should send without panic
	Info().Str("key", "val").Msg("hello")
}

func TestEventMsgf(t *testing.T) {
	resetLogger()
	Init(false)
	defer resetLogger()

	// Msgf should format and send without panic
	Info().Msgf("hello %s %d", "world", 42)
}

func TestEventMsg_NilFields(t *testing.T) {
	resetLogger()
	Init(false)
	defer resetLogger()

	// An event with nil fields should still send without panic
	e := &Event{level: zerolog.InfoLevel, fields: nil}
	e.Msg("nil fields message")
}

func TestEvent_EnsureFields_AlreadyInitialized(t *testing.T) {
	e := &Event{fields: map[string]interface{}{"existing": "val"}}
	e.ensureFields()
	assert.Equal(t, "val", e.fields["existing"])
}

func TestRingBuffer_Reset(t *testing.T) {
	rb := NewRingBuffer(5)
	rb.Add(zerolog.InfoLevel, "msg1", nil)
	rb.Add(zerolog.WarnLevel, "msg2", nil)

	entries := rb.Get(zerolog.NoLevel)
	assert.Len(t, entries, 2)

	rb.Reset()

	entries = rb.Get(zerolog.NoLevel)
	assert.Empty(t, entries)
}

func TestAsyncWriter_ProcessesEntries(t *testing.T) {
	resetLogger()
	Init(false)
	defer resetLogger()

	// Send a few log entries and then shut down; asyncWriter must drain them.
	Info().Str("test", "1").Msg("entry one")
	Warn().Str("test", "2").Msg("entry two")
	Error().Str("test", "3").Msg("entry three")

	// Shutdown flushes remaining entries; if asyncWriter is broken this would hang.
	Shutdown()
	initialized = false
}
