//go:build linux

package testclient

import "testing"

func TestTestLogindPropertiesUseExpectedTypes(t *testing.T) {
	properties := testLogindProperties{}
	preparing, err := properties.Get(testLogindInterface, "PreparingForSleep")
	if err != nil {
		t.Fatal("PreparingForSleep was unavailable")
	}
	if value, ok := preparing.Value().(bool); !ok || value {
		t.Fatal("PreparingForSleep did not return false as a boolean")
	}
	delay, err := properties.Get(testLogindInterface, "InhibitDelayMaxUSec")
	if err != nil {
		t.Fatal("InhibitDelayMaxUSec was unavailable")
	}
	if value, ok := delay.Value().(uint64); !ok || value == 0 {
		t.Fatal("InhibitDelayMaxUSec did not return a positive uint64")
	}
	if _, err := properties.Get(testLogindInterface, "Unknown"); err == nil {
		t.Fatal("unknown logind property was accepted")
	}
}
