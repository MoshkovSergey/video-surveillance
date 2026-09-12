package httpapi

import "log/slog"

// nilLogger возвращает discard-логгер для компонентов без явного логгера.
func nilLogger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(discardWriter{}, nil))
}

type discardWriter struct{}

func (discardWriter) Write(p []byte) (int, error) { return len(p), nil }