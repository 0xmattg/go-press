package logger

import (
	"log/slog"
	"os"
)

var defaultLogger *slog.Logger

func init() {
	defaultLogger = slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
}

// Setup initializes the global logger with the given level.
func Setup(level string) {
	var lvl slog.Level
	switch level {
	case "debug":
		lvl = slog.LevelDebug
	case "warn":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}
	defaultLogger = slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: lvl,
	}))
	slog.SetDefault(defaultLogger)
}

// Get returns the global logger.
func Get() *slog.Logger {
	return defaultLogger
}

// Debug logs at debug level.
func Debug(msg string, args ...any) { defaultLogger.Debug(msg, args...) }

// Info logs at info level.
func Info(msg string, args ...any) { defaultLogger.Info(msg, args...) }

// Warn logs at warn level.
func Warn(msg string, args ...any) { defaultLogger.Warn(msg, args...) }

// Error logs at error level.
func Error(msg string, args ...any) { defaultLogger.Error(msg, args...) }
