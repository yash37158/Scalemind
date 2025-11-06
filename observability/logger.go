package observability

import (
	"os"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// InitializeLogger sets up structured JSON logging
func InitializeLogger(level string) (*zap.Logger, error) {
	var zapLevel zapcore.Level
	switch level {
	case "debug":
		zapLevel = zapcore.DebugLevel
	case "info":
		zapLevel = zapcore.InfoLevel
	case "warn":
		zapLevel = zapcore.WarnLevel
	case "error":
		zapLevel = zapcore.ErrorLevel
	default:
		zapLevel = zapcore.InfoLevel
	}

	config := zap.NewProductionConfig()
	config.Level = zap.NewAtomicLevelAt(zapLevel)
	config.EncoderConfig.TimeKey = "timestamp"
	config.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
	config.EncoderConfig.MessageKey = "message"
	config.EncoderConfig.LevelKey = "level"
	config.EncoderConfig.CallerKey = "caller"

	// Use console encoder in development
	if os.Getenv("ENV") == "development" {
		config.Encoding = "console"
		config.EncoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
	}

	// CRITICAL: Output logs to stderr, not stdout
	// MCP protocol uses stdout for JSON-RPC communication
	config.OutputPaths = []string{"stderr"}
	config.ErrorOutputPaths = []string{"stderr"}

	logger, err := config.Build()
	if err != nil {
		return nil, err
	}

	// Replace global logger
	zap.ReplaceGlobals(logger)

	return logger, nil
}

// Logger returns the global logger instance
func Logger() *zap.Logger {
	return zap.L()
}

// LoggerWithTraceID adds a trace ID to the logger context
func LoggerWithTraceID(traceID string) *zap.Logger {
	return zap.L().With(zap.String("trace_id", traceID))
}

