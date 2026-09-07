package client

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"testing"
	"time"
)

func TestFlowStatusCountsLossAndExpiryWithoutTraffic(t *testing.T) {
	c := &flowCollector{}
	now := time.Now()
	c.policy(flowPolicy(now, 7), now)
	c.observe([]byte("invalid packet"), true, now)
	for i := 0; i < maxFlowWindows+3; i++ {
		c.observe(applicationTCPPacket("100.64.0.1", "100.64.0.2", 1234, uint16(i)), true, now)
	}
	c.reportResult(false)
	c.reportResult(true)
	c.policyFailed()
	status := c.status(now)
	if status.CapacityDrops != 3 || status.UnsupportedPackets != 1 || status.DroppedPackets != 4 || status.ReportAttempts != 2 || status.ReportFailures != 1 || status.PolicyFailures != 1 || status.AcknowledgedWindows != 0 {
		t.Fatalf("loss counters: %+v", status)
	}
	window, version := c.next(now.Add(11 * time.Second))
	c.acknowledge(window.GetWindowId(), version)
	c.acknowledge(window.GetWindowId(), version)
	status = c.status(now.Add(time.Minute))
	if status.Enabled || status.ActiveWindows != 0 || status.PendingWindows != 0 || status.DiscardedWindows != maxFlowWindows-1 || status.AcknowledgedWindows != 1 {
		t.Fatalf("idle expiry/ack counters: %+v", status)
	}
	if c.status(now.Add(2*time.Minute)) != status {
		t.Fatal("repeated status duplicated losses")
	}
}

func TestFlowStatusLogContainsOnlyOperationalNumbers(t *testing.T) {
	c := &flowCollector{}
	now := time.Now()
	c.policy(flowPolicy(now, 7), now)
	c.observe(applicationTCPPacket("100.64.0.1", "100.64.0.2", 1234, 443), true, now)
	var buffer bytes.Buffer
	logFlowStatus(slog.New(slog.NewJSONHandler(&buffer, nil)), c.status(now))
	var fields map[string]any
	if err := json.Unmarshal(buffer.Bytes(), &fields); err != nil {
		t.Fatal(err)
	}
	for name, value := range fields {
		switch name {
		case "time", "level", "msg":
			continue
		case "enabled":
			if _, ok := value.(bool); !ok {
				t.Fatal("invalid enabled value")
			}
		default:
			if _, ok := value.(float64); !ok {
				t.Fatalf("non-numeric flow metadata leaked in %s", name)
			}
		}
	}
	if bytes.Contains(buffer.Bytes(), []byte("100.64.")) {
		t.Fatal("IP address leaked")
	}
}
