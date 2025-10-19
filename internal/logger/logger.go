package logger

import (
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/rs/zerolog"
)

const (
	colorBlack = iota + 30
	colorRed
	colorGreen
	colorYellow
	colorBlue
	colorMagenta
	colorCyan
	colorWhite

	colorBold     = 1
	colorDarkGray = 90
)

var (
	once   sync.Once
	logger *zerolog.Logger
)

// Get returns the singleton logger instance, initializing it on first call.
func Get() *zerolog.Logger {
	once.Do(func() {
		logger = newLogger()
	})
	return logger
}

func colorize(s interface{}, c int) string {
	return fmt.Sprintf("\x1b[%dm%v\x1b[0m", c, s)
}

// new creates a logger based on the ENV environment variable
func newLogger() *zerolog.Logger {
	env := os.Getenv("ENV")

	// Set log level based on LOG_LEVEL env var, default to info
	logLevel := zerolog.InfoLevel
	if levelStr := os.Getenv("LOG_LEVEL"); levelStr != "" {
		if parsedLevel, err := zerolog.ParseLevel(strings.ToLower(levelStr)); err == nil {
			logLevel = parsedLevel
		} else {
			fmt.Fprintf(os.Stderr, "Invalid LOG_LEVEL \"%s\"; defaulting to 'info'\n", levelStr)
		}
	}

	zerolog.SetGlobalLevel(logLevel)

	if env == "development" || env == "dev" || env == "" {
		return newDevelopment()
	}
	return newProduction()
}

// newDevelopment creates a development logger with console output and colors
func newDevelopment() *zerolog.Logger {
	output := zerolog.ConsoleWriter{
		Out:        os.Stderr,
		TimeFormat: "2006-01-02 15:04:05",
		FormatLevel: func(i interface{}) string {
			var l string
			if ll, ok := i.(string); ok {
				switch ll {
				case "trace":
					l = colorize("TRC", colorMagenta)
				case "debug":
					l = colorize("DBG", colorYellow)
				case "info":
					l = colorize("INF", colorGreen)
				case "warn":
					l = colorize("WRN", colorRed)
				case "error":
					l = colorize("ERR", colorRed)
				case "fatal":
					l = colorize("FTL", colorRed)
				case "panic":
					l = colorize("PNC", colorRed)
				default:
					l = colorize(strings.ToUpper(ll)[0:3], colorBold)
				}
			} else {
				l = strings.ToUpper(fmt.Sprintf("%s", i))[0:3]
			}
			return l
		},
	}

	zl := zerolog.New(output).With().Timestamp().Logger()
	return &zl
}

// newProduction creates a production logger with JSON output and UNIX timestamps
func newProduction() *zerolog.Logger {
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	zl := zerolog.New(os.Stderr).With().Timestamp().Logger()
	return &zl
}
