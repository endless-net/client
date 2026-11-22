package clientapp

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/endless-net/client/internal/localapi"
)

const recentLogCapacity = 256

// Keep the same producer-redacted records as the JSON sink. Routine successful
// status polling and log reads stay in stderr without displacing diagnostics.
func (q *logQueue) retain(data []byte) {
	var event struct {
		Time      time.Time
		Operation string
		Outcome   string
	}
	if len(data) > 2048 || json.Unmarshal(data, &event) != nil || event.Time.IsZero() {
		return
	}
	if event.Outcome == "success" {
		switch event.Operation {
		case "GetStatus", "GetRuntimeInfo", "WatchEvents", "ListRecentLogs":
			return
		}
	}
	q.logSequence++
	q.recentLogs[(q.logSequence-1)%recentLogCapacity] = localapi.RecentLog{
		Sequence: q.logSequence, Time: event.Time, Message: strings.TrimSpace(string(data)),
	}
}

func (t *Telemetry) RecentLogs() []localapi.RecentLog {
	q := t.logs
	q.mu.Lock()
	defer q.mu.Unlock()
	size := min(q.logSequence, recentLogCapacity)
	result := make([]localapi.RecentLog, 0, size)
	for index := q.logSequence - size; index < q.logSequence; index++ {
		result = append(result, q.recentLogs[index%recentLogCapacity])
	}
	return result
}
