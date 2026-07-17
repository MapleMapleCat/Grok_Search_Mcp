package panel

import (
	"net/http"
	"time"

	"github.com/grok-mcp/internal/store"
)

type operationalMetricsResponse struct {
	CapturedAt  time.Time                   `json:"captured_at"`
	SQLite      store.SQLiteMetricsSnapshot `json:"sqlite"`
	UsageWriter store.AsyncUsageWriterStats `json:"usage_writer"`
}

func (handler *Handler) adminOperationalMetrics(writer http.ResponseWriter, _ *http.Request) {
	if handler.SQLiteMetrics == nil || handler.UsageWriterMetrics == nil {
		writeError(writer, http.StatusServiceUnavailable, "operational metrics are unavailable")
		return
	}

	writeJSON(writer, http.StatusOK, operationalMetricsResponse{
		CapturedAt:  time.Now().UTC(),
		SQLite:      handler.SQLiteMetrics.SQLiteMetrics(),
		UsageWriter: handler.UsageWriterMetrics.Stats(),
	})
}
