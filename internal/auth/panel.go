package auth

import (
	"crypto/subtle"
	"net/http"
	"strings"
)

const panelKeyHeader = "X-Panel-Key"

// PanelKeyMiddleware 校验面板 API 请求头 X-Panel-Key。
func PanelKeyMiddleware(panelKey string) func(http.Handler) http.Handler {
	expected := strings.TrimSpace(panelKey)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			got := strings.TrimSpace(r.Header.Get(panelKeyHeader))
			if got == "" || subtle.ConstantTimeCompare([]byte(got), []byte(expected)) != 1 {
				http.Error(w, "missing or invalid panel key", http.StatusUnauthorized)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}