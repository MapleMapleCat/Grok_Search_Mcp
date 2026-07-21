package panel

import (
	"log"
	"net/http"
	"time"

	"github.com/MapleMapleCat/Grok_Search_Mcp/internal/ratelimit"
	"github.com/MapleMapleCat/Grok_Search_Mcp/internal/store"
)

type operationalMetricsResponse struct {
	CapturedAt  time.Time                          `json:"captured_at"`
	SQLite      store.SQLiteMetricsSnapshot        `json:"sqlite"`
	UsageWriter store.AsyncUsageWriterStats        `json:"usage_writer"`
	IPLimiter   ratelimit.IPLimiterMetricsSnapshot `json:"ip_limiter"`
}

func (handler *Handler) adminOperationalMetrics(writer http.ResponseWriter, request *http.Request) {
	if handler.Store == nil {
		writeError(writer, http.StatusServiceUnavailable, "operational metrics are unavailable")
		return
	}

	settings, err := handler.loadEffectiveServerSettings(request)
	if err != nil {
		log.Printf("load operational metrics setting failed: %v", err)
		writeError(writer, http.StatusInternalServerError, "failed to load operational metrics setting")
		return
	}
	if !settings.Runtime.OperationsMetricsEnabled {
		writeError(writer, http.StatusNotFound, "operational metrics are disabled")
		return
	}
	if handler.SQLiteMetrics == nil || handler.UsageWriterMetrics == nil || handler.IPLimiterMetrics == nil {
		writeError(writer, http.StatusServiceUnavailable, "operational metrics are unavailable")
		return
	}

	writeJSON(writer, http.StatusOK, operationalMetricsResponse{
		CapturedAt:  time.Now().UTC(),
		SQLite:      handler.SQLiteMetrics.SQLiteMetrics(),
		UsageWriter: handler.UsageWriterMetrics.Stats(),
		IPLimiter:   handler.IPLimiterMetrics.Metrics(),
	})
}
