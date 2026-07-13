package usage

import "bytes"

const maxOutcomeInspectionBytes = 256 << 10

// responseOutcomeInspector incrementally inspects a bounded response prefix.
// It preserves partial SSE lines across writes and permanently latches errors.
type responseOutcomeInspector struct {
	inspectedBytes int
	jsonCapture    []byte
	sseLineBuffer  []byte
	latchedError   bool
	finalized      bool
}

func (inspector *responseOutcomeInspector) inspect(body []byte) {
	if inspector == nil || inspector.finalized || len(body) == 0 || inspector.inspectedBytes >= maxOutcomeInspectionBytes {
		return
	}
	remainingBytes := maxOutcomeInspectionBytes - inspector.inspectedBytes
	if len(body) > remainingBytes {
		body = body[:remainingBytes]
	}
	inspector.inspectedBytes += len(body)
	inspector.jsonCapture = append(inspector.jsonCapture, body...)

	for _, currentByte := range body {
		if currentByte == '\n' {
			inspector.inspectSSELine(inspector.sseLineBuffer)
			inspector.sseLineBuffer = inspector.sseLineBuffer[:0]
			continue
		}
		inspector.sseLineBuffer = append(inspector.sseLineBuffer, currentByte)
	}
}

func (inspector *responseOutcomeInspector) finalize() {
	if inspector == nil || inspector.finalized {
		return
	}
	inspector.finalized = true
	if len(inspector.sseLineBuffer) > 0 {
		inspector.inspectSSELine(inspector.sseLineBuffer)
	}
	if !inspector.latchedError && jsonRPCPayloadHasToolError(bytes.TrimSpace(inspector.jsonCapture)) {
		inspector.latchedError = true
	}
}

func (inspector *responseOutcomeInspector) inspectSSELine(line []byte) {
	if inspector.latchedError {
		return
	}
	line = bytes.TrimSpace(line)
	if !bytes.HasPrefix(line, []byte("data:")) {
		return
	}
	payload := bytes.TrimSpace(bytes.TrimPrefix(line, []byte("data:")))
	if len(payload) > 0 && jsonRPCPayloadHasToolError(payload) {
		inspector.latchedError = true
	}
}

func (inspector *responseOutcomeInspector) toolError() bool {
	inspector.finalize()
	return inspector.latchedError
}
