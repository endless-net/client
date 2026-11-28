package clientapp

import "testing"

func TestSystemSerialUsesTypeOneAndBoundsStringArea(t *testing.T) {
	for _, tc := range []struct {
		raw  []byte
		want string
	}{
		{[]byte{0, 4, 0, 0, 0, 0, 1, 8, 0, 0, 0, 0, 0, 2, 'a', 0, 'Z', '4', 0, 0}, "Z4"},
		{[]byte{1, 8, 0, 0, 0, 0, 0, 2, 'a', 0, 0}, ""},
		{[]byte{1, 8, 0, 0, 0, 0, 0, 1, 'x', 0}, ""},
		{[]byte{1, 2, 0, 0}, ""},
	} {
		if got := smbiosSystemSerial(tc.raw); got != tc.want {
			t.Fatalf("serial from fixture %q want %q", got, tc.want)
		}
	}
}
