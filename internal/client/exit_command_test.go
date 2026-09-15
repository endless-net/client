package client

import (
	"errors"
	"testing"
)

func TestExitCommandOutputCancelsOnOverflowWithoutAcceptingTruncation(t *testing.T) {
	for _, chunks := range [][]string{{"123456789"}, {"1234", "5678", "9"}, {"12345678", "9", "more"}} {
		cancelled := 0
		output := &exitCommandOutput{limit: 8, cancel: func() { cancelled++ }}
		for _, chunk := range chunks {
			before := string(output.data)
			n, err := output.Write([]byte(chunk))
			if len(before)+len(chunk) > 8 || output.overflow {
				if n != 0 || !errors.Is(err, errExitCommandOutputLimit) || string(output.data) != before || cancelled != 1 {
					t.Fatal("overflow was accepted, retained or did not cancel", n, err, cancelled)
				}
			} else if n != len(chunk) || err != nil || cancelled != 0 || string(output.data) != before+chunk {
				t.Fatal("bounded output changed", n, err, cancelled)
			}
		}
		if !output.overflow || len(output.data) > output.limit || cancelled != 1 {
			t.Fatal("overflow did not retain bounded failed state")
		}
	}
}
