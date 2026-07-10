package logx

import (
	"bytes"
	"log"
	"strings"
	"testing"
)

func TestTruncate(t *testing.T) {
	if got := Truncate("hello", 10); got != "hello" {
		t.Fatalf("got %q, want %q", got, "hello")
	}
	if got := Truncate("hello", 5); got != "hello" {
		t.Fatalf("got %q, want %q", got, "hello")
	}
	if got := Truncate("hello world", 5); got != "hello..." {
		t.Fatalf("got %q, want %q", got, "hello...")
	}
	if got := Truncate("", 5); got != "" {
		t.Fatalf("got %q, want empty", got)
	}
}

func TestDebugfDisabled(t *testing.T) {
	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer log.SetOutput(nil)

	l := New("test", false)
	l.Debugf("should not appear %d", 1)
	if buf.Len() != 0 {
		t.Fatalf("expected no output when disabled, got %q", buf.String())
	}
}

func TestDebugfEnabled(t *testing.T) {
	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer log.SetOutput(nil)

	l := New("test", true)
	l.Debugf("hello %s", "world")
	got := buf.String()
	if !strings.Contains(got, "[test]") {
		t.Fatalf("expected prefix [test], got %q", got)
	}
	if !strings.Contains(got, "hello world") {
		t.Fatalf("expected message, got %q", got)
	}
}

func TestDebugStateControlsSharedLoggers(t *testing.T) {
	var buffer bytes.Buffer
	previousWriter := log.Writer()
	previousFlags := log.Flags()
	previousPrefix := log.Prefix()
	log.SetOutput(&buffer)
	log.SetFlags(0)
	log.SetPrefix("")
	t.Cleanup(func() {
		log.SetOutput(previousWriter)
		log.SetFlags(previousFlags)
		log.SetPrefix(previousPrefix)
	})

	debugState := NewDebugState(false)
	mcpLogger := NewWithDebugState("mcp", debugState)
	grokLogger := NewWithDebugState("grok", debugState)

	mcpLogger.Debugf("disabled MCP message")
	grokLogger.Debugf("disabled Grok message")
	if buffer.Len() != 0 {
		t.Fatalf("expected shared loggers to be disabled, got %q", buffer.String())
	}

	debugState.SetEnabled(true)
	mcpLogger.Debugf("enabled MCP message")
	grokLogger.Debugf("enabled Grok message")
	loggedOutput := buffer.String()
	for _, expectedText := range []string{"[mcp] enabled MCP message", "[grok] enabled Grok message"} {
		if !strings.Contains(loggedOutput, expectedText) {
			t.Fatalf("expected output to contain %q, got %q", expectedText, loggedOutput)
		}
	}

	buffer.Reset()
	debugState.SetEnabled(false)
	mcpLogger.Debugf("disabled again")
	grokLogger.Debugf("disabled again")
	if buffer.Len() != 0 {
		t.Fatalf("expected shared loggers to stop immediately, got %q", buffer.String())
	}
}

func TestDebugfNilReceiver(t *testing.T) {
	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer log.SetOutput(nil)

	var l *Logger
	l.Debugf("should not appear")
	if buf.Len() != 0 {
		t.Fatalf("expected no output for nil receiver, got %q", buf.String())
	}
}
