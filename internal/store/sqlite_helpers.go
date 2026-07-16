package store

import (
	"crypto/rand"
	"fmt"
	"time"
)

// timeLayout 为库内 UTC 时间字符串格式（与 SQLite datetime 列一致）。
const timeLayout = "2006-01-02 15:04:05"

// randomID 生成 UUID v4 风格的十六进制 ID（无第三方依赖）。
func randomID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}

func parseTime(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}
	return time.ParseInLocation(timeLayout, s, time.UTC)
}

func formatTime(t time.Time) string {
	return t.UTC().Format(timeLayout)
}

func normalizePanelPageLimit(limit int) int {
	if limit <= 0 {
		return 50
	}
	if limit > 100 {
		return 100
	}
	return limit
}
