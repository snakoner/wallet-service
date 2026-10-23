package logger

import (
	"io"
	"os"
	"strings"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

type Logger struct {
	zerolog.Logger
}

type Config struct {
	Level       string
	ServiceName string
	Environment string
	Pretty      bool
}

func New(cfg Config) *Logger {
	var output io.Writer = os.Stdout
	if cfg.Pretty {
		output = zerolog.ConsoleWriter{
			Out:        os.Stdout,
			TimeFormat: time.RFC3339,
		}
	}

	level := parseLogLevel(cfg.Level)

	zlog := zerolog.New(output).
		Level(level).
		With().
		Timestamp().
		Str("service", cfg.ServiceName).
		Str("environment", cfg.Environment).
		Logger()

	log.Logger = zlog

	return &Logger{Logger: zlog}
}

func (l *Logger) WithField(key string, value any) *Logger {
	newLogger := l.With().Interface(key, value).Logger()

	return &Logger{Logger: newLogger}
}

func (l *Logger) WithFields(fields map[string]any) *Logger {
	ctx := l.With()
	for k, v := range fields {
		ctx = ctx.Interface(k, v)
	}
	newLogger := ctx.Logger()

	return &Logger{Logger: newLogger}
}

func parseLogLevel(level string) zerolog.Level {
	switch strings.ToLower(level) {
	case "debug":
		return zerolog.DebugLevel
	case "info":
		return zerolog.InfoLevel
	case "warn", "warning":
		return zerolog.WarnLevel
	case "error":
		return zerolog.ErrorLevel
	case "fatal":
		return zerolog.FatalLevel
	case "panic":
		return zerolog.PanicLevel
	default:
		return zerolog.InfoLevel
	}
}
