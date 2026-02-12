// Package logging provides shared logging configuration.
package logging

import (
	"log/slog"
	"os"
)

const defaultLogLevel = slog.LevelWarn

// Init sets the log level based on the "LOG_LEVEL" environment variable.
func Init() {
	level := defaultLogLevel
	if levelText, ok := os.LookupEnv("LOG_LEVEL"); ok {
		if err := level.UnmarshalText([]byte(levelText)); err != nil {
			level = slog.LevelDebug
		}
	}
	handler := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})
	slog.SetDefault(slog.New(handler))
}
