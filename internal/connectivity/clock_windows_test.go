//go:build windows

package connectivity

import (
	"testing"
	"time"
)

func TestWindowsClockUsesNativePreciseCounterWithoutChangingSystemTime(t *testing.T) {
	c, err := NewWindowsClock("native-clock-fixture")
	if err != nil {
		t.Fatal(err)
	}
	first, err := c.Sample()
	if err != nil || !first.valid() {
		t.Fatal("native clock sample unavailable", err)
	}
	second, err := c.Sample()
	if err != nil || !second.valid() || second.Uptime < first.Uptime || second.Wall.Before(first.Wall) || second.Epoch != first.Epoch {
		t.Fatal("native clock regressed", err)
	}
	if delta := time.Since(second.Wall); delta < 0 || delta > time.Second {
		t.Fatal("native sample not current wall time")
	}
	if _, err := NewWindowsClock(""); err != ErrConfig {
		t.Fatal("empty runtime epoch accepted")
	}
}
