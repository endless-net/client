package main

import (
	"errors"
	"testing"
)

func TestUnavailableStagesRetainNetworkOutcome(t *testing.T) {
	for stage, err := range map[string]error{"dial": errDialUnavailable, "write": errWriteUnavailable, "read": errReadUnavailable} {
		if !errors.Is(err, errUnreachable) || err.Error() != "application exchange unavailable: "+stage {
			t.Fatalf("stage %s changed the classified outcome", stage)
		}
	}
}
