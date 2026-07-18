package panel

import (
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"strings"
)

func writeJSON(writer http.ResponseWriter, status int, value any) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(value)
}

type errorResponse struct {
	Code  string `json:"code"`
	Error string `json:"error"`
}

func writeError(writer http.ResponseWriter, status int, message string) {
	writeJSON(writer, status, errorResponse{
		Code:  defaultErrorCode(status),
		Error: message,
	})
}

func defaultErrorCode(status int) string {
	switch status {
	case http.StatusBadRequest:
		return "invalid_request"
	case http.StatusUnauthorized:
		return "unauthorized"
	case http.StatusForbidden:
		return "forbidden"
	case http.StatusNotFound:
		return "not_found"
	case http.StatusMethodNotAllowed:
		return "method_not_allowed"
	case http.StatusConflict:
		return "conflict"
	case http.StatusRequestEntityTooLarge:
		return "payload_too_large"
	case http.StatusUnsupportedMediaType:
		return "unsupported_media_type"
	case http.StatusTooManyRequests:
		return "rate_limited"
	case http.StatusBadGateway:
		return "upstream_error"
	case http.StatusServiceUnavailable:
		return "unavailable"
	case http.StatusInternalServerError:
		return "internal_error"
	default:
		return "request_failed"
	}
}

func decodeJSONBody(writer http.ResponseWriter, request *http.Request, destination any) bool {
	contentType := strings.TrimSpace(request.Header.Get("Content-Type"))
	if contentType != "" {
		mediaType, _, err := mime.ParseMediaType(contentType)
		if err != nil || !strings.EqualFold(mediaType, "application/json") {
			writeError(writer, http.StatusUnsupportedMediaType, "content type must be application/json")
			return false
		}
	}

	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		writeError(writer, http.StatusBadRequest, "invalid JSON body")
		return false
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		writeError(writer, http.StatusBadRequest, "invalid JSON body")
		return false
	}
	return true
}

const maxPanelBodyBytes = 1 << 20 // 1 MiB

// MaxPanelBodyBytes 返回面板 API 默认请求体上限。
func MaxPanelBodyBytes() int64 { return maxPanelBodyBytes }

// MaxBodyMiddleware 限制 JSON 请求体大小，防止恶意超大请求耗尽内存。
func MaxBodyMiddleware(limit int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			request.Body = http.MaxBytesReader(writer, request.Body, limit)
			next.ServeHTTP(writer, request)
		})
	}
}
