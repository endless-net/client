//go:build windows

package client

import (
	"io"
	"log"
	"strings"
	"sync"

	"golang.org/x/sys/windows/svc/eventlog"
)

func ConfigureWindowsEventLogger(source string) (func(), error) {
	source = strings.TrimSpace(source)
	if source == "" {
		source = DefaultWindowsEventLogSource
	}
	eventLog, err := eventlog.Open(source)
	if err != nil {
		return nil, err
	}
	previous := log.Writer()
	writer := &windowsEventLogWriter{log: eventLog}
	log.SetOutput(io.MultiWriter(previous, writer))
	return func() {
		log.SetOutput(previous)
		_ = eventLog.Close()
	}, nil
}

type windowsEventLogWriter struct {
	mu  sync.Mutex
	log *eventlog.Log
}

func (w *windowsEventLogWriter) Write(p []byte) (int, error) {
	message := strings.TrimSpace(RedactServiceLogMessage(string(p)))
	if message == "" {
		return len(p), nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	lower := strings.ToLower(message)
	if strings.Contains(lower, "failed") || strings.Contains(lower, "error") {
		_ = w.log.Error(2, message)
	} else {
		_ = w.log.Info(1, message)
	}
	return len(p), nil
}
