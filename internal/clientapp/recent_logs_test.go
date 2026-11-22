package clientapp

import (
	"fmt"
	"testing"
)

func TestRecentLogsRetentionAndPollingFilter(t *testing.T) {
	q := &logQueue{}
	// Exercise the retention path independently of the asynchronous writer.
	// Direct calls here hold the same lock as Write.
	q.mu.Lock()
	for index := 0; index < recentLogCapacity+1; index++ {
		q.retain(fmt.Appendf(nil, "{\"time\":\"2026-11-22T10:00:00Z\",\"msg\":\"event\",\"index\":%d}", index))
	}
	q.retain([]byte("{\"time\":\"2026-11-22T10:00:00Z\",\"operation\":\"GetStatus\",\"outcome\":\"success\"}"))
	q.mu.Unlock()
	logs := (&Telemetry{logs: q}).RecentLogs()
	if len(logs) != recentLogCapacity || logs[0].Sequence != 2 || logs[len(logs)-1].Sequence != 257 {
		t.Fatalf("unexpected retained range: %d events", len(logs))
	}
}
