package usage

import (
	"context"
	"sync/atomic"
)

type toolOutcomeContextKey struct{}

const (
	toolOutcomeUnknown uint32 = iota
	toolOutcomeSuccess
	toolOutcomeError
)

// ToolOutcomeMarker records the semantic result produced by an MCP tool
// handler. The marker is safe to update and read across goroutines.
type ToolOutcomeMarker struct {
	state atomic.Uint32
}

// WithToolOutcomeMarker installs an initially-unknown marker in the request
// context so MCP handlers and usage middleware can share the semantic result.
func WithToolOutcomeMarker(ctx context.Context) (context.Context, *ToolOutcomeMarker) {
	marker := &ToolOutcomeMarker{}
	return context.WithValue(ctx, toolOutcomeContextKey{}, marker), marker
}

// MarkToolOutcome records the authoritative semantic outcome of a tool
// handler. Calls made without an installed marker are harmless no-ops.
func MarkToolOutcome(ctx context.Context, success bool) {
	marker, ok := ctx.Value(toolOutcomeContextKey{}).(*ToolOutcomeMarker)
	if !ok || marker == nil {
		return
	}
	if success {
		marker.state.CompareAndSwap(toolOutcomeUnknown, toolOutcomeSuccess)
		return
	}
	marker.state.Store(toolOutcomeError)
}

// Outcome returns the recorded result and whether a handler set it.
func (m *ToolOutcomeMarker) Outcome() (success bool, known bool) {
	if m == nil {
		return false, false
	}
	switch m.state.Load() {
	case toolOutcomeSuccess:
		return true, true
	case toolOutcomeError:
		return false, true
	default:
		return false, false
	}
}
