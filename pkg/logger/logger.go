// ==============================================================================
// LOGGER PACKAGE - pkg/logger/logger.go
// ==============================================================================
package logger

import (
	"encoding/json"
	"log"
	"os"
	"time"
)

type Logger interface {
	Info(message string, fields map[string]interface{})
	Error(message string, fields map[string]interface{})
	Warn(message string, fields map[string]interface{})
	Debug(message string, fields map[string]interface{})
	Fatal(message string, fields map[string]interface{})
}

type jsonLogger struct {
	serviceName string
	logger      *log.Logger
}

func New(serviceName string) Logger {
	return &jsonLogger{
		serviceName: serviceName,
		logger:      log.New(os.Stdout, "", 0),
	}
}

func (l *jsonLogger) log(level, message string, fields map[string]interface{}) {
	entry := map[string]interface{}{
		"timestamp": time.Now().UTC().Format(time.RFC3339),
		"level":     level,
		"service":   l.serviceName,
		"message":   message,
	}

	for k, v := range fields {
		entry[k] = v
	}

	jsonData, _ := json.Marshal(entry)
	l.logger.Println(string(jsonData))
}

func (l *jsonLogger) Info(message string, fields map[string]interface{}) {
	l.log("info", message, fields)
}

func (l *jsonLogger) Error(message string, fields map[string]interface{}) {
	l.log("error", message, fields)
}

func (l *jsonLogger) Warn(message string, fields map[string]interface{}) {
	l.log("warn", message, fields)
}

func (l *jsonLogger) Debug(message string, fields map[string]interface{}) {
	l.log("debug", message, fields)
}

func (l *jsonLogger) Fatal(message string, fields map[string]interface{}) {
	l.log("fatal", message, fields)
	os.Exit(1)
}

func NewNop() Logger {
	return &nopLogger{}
}

type nopLogger struct{}

func (l *nopLogger) Info(message string, fields map[string]interface{})  {}
func (l *nopLogger) Error(message string, fields map[string]interface{}) {}
func (l *nopLogger) Warn(message string, fields map[string]interface{})  {}
func (l *nopLogger) Debug(message string, fields map[string]interface{}) {}
func (l *nopLogger) Fatal(message string, fields map[string]interface{}) {}
