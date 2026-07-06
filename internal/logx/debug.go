// Package logx 提供可按开关启用的调试日志，避免在生产环境刷屏。
package logx

import (
	"log"
	"log/slog"
	"os"
)

// defaultLogger 是包级 slog 实例，输出到 stderr，级别 Info，
// 供需要结构化日志的调用方使用；debug 路径仍走传统 log.Printf（见 Logger）。
var defaultLogger = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
	Level: slog.LevelInfo,
}))

// DefaultLogger 返回包级 slog.Logger，供面板/进程入口等做结构化日志。
func DefaultLogger() *slog.Logger {
	return defaultLogger
}

// Logger 在 enabled 为 false 时忽略所有 Debugf 调用。
type Logger struct {
	enabled bool
	prefix  string
}

// New 创建带 [prefix] 前缀的调试日志器。
func New(prefix string, enabled bool) *Logger {
	return &Logger{enabled: enabled, prefix: prefix}
}

// Debugf 等价于 log.Printf，仅在调试模式开启时输出。
func (l *Logger) Debugf(format string, args ...any) {
	if l == nil || !l.enabled {
		return
	}
	log.Printf("["+l.prefix+"] "+format, args...)
}

// Truncate 将字符串截断到 max 字节长度，超出部分以 "..." 结尾（用于日志脱敏/限长）。
func Truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
