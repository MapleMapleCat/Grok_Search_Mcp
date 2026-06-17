package logx

import "log"

type Logger struct {
	enabled bool
	prefix  string
}

func New(prefix string, enabled bool) *Logger {
	return &Logger{enabled: enabled, prefix: prefix}
}

func (l *Logger) Debugf(format string, args ...any) {
	if l == nil || !l.enabled {
		return
	}
	log.Printf("["+l.prefix+"] "+format, args...)
}

func Truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}