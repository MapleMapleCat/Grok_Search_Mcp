package panel

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWriteErrorIncludesStableCodeAndEnglishMessage(t *testing.T) {
	responseRecorder := httptest.NewRecorder()

	writeError(responseRecorder, http.StatusTooManyRequests, "rate limit exceeded")

	if responseRecorder.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want %d", responseRecorder.Code, http.StatusTooManyRequests)
	}
	if contentType := responseRecorder.Header().Get("Content-Type"); contentType != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", contentType)
	}

	var response errorResponse
	if err := json.NewDecoder(responseRecorder.Body).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if response.Code != "rate_limited" {
		t.Fatalf("code = %q, want rate_limited", response.Code)
	}
	if response.Error != "rate limit exceeded" {
		t.Fatalf("error = %q, want rate limit exceeded", response.Error)
	}
}

func TestDefaultErrorCodeCoversPublicPanelStatuses(t *testing.T) {
	testCases := map[int]string{
		http.StatusBadRequest:            "invalid_request",
		http.StatusUnauthorized:          "unauthorized",
		http.StatusForbidden:             "forbidden",
		http.StatusNotFound:              "not_found",
		http.StatusConflict:              "conflict",
		http.StatusRequestEntityTooLarge: "payload_too_large",
		http.StatusUnsupportedMediaType:  "unsupported_media_type",
		http.StatusTooManyRequests:       "rate_limited",
		http.StatusInternalServerError:   "internal_error",
		http.StatusBadGateway:            "upstream_error",
		http.StatusServiceUnavailable:    "unavailable",
	}

	for status, expectedCode := range testCases {
		if actualCode := defaultErrorCode(status); actualCode != expectedCode {
			t.Errorf("status %d code = %q, want %q", status, actualCode, expectedCode)
		}
	}
}
