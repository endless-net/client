package rpcutil

import "testing"

func TestValidUUID(t *testing.T) {
	for _, tc := range []struct {
		name, value string
		want        bool
	}{
		{"lowercase", "abcdef01-2345-4678-9abc-def012345678", true},
		{"uppercase", "ABCDEF01-2345-4678-9ABC-DEF012345678", true},
		{"unrestricted-version-and-variant", "00000000-0000-0000-0000-000000000001", true},
		{"zero", "00000000-0000-0000-0000-000000000000", false},
		{"empty", "", false},
		{"short", "abcdef01-2345-4678-9abc-def01234567", false},
		{"long", "abcdef01-2345-4678-9abc-def0123456789", false},
		{"compact", "abcdef01234546789abcdef012345678", false},
		{"separator", "abcdef01_2345-4678-9abc-def012345678", false},
		{"extra-separator", "abcdef01-2345-4678-9abc-def0123456-8", false},
		{"invalid-hex", "gbcdef01-2345-4678-9abc-def012345678", false},
		{"whitespace", " abcdef01-2345-4678-9abc-def012345678", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := ValidUUID(tc.value); got != tc.want {
				t.Fatalf("ValidUUID(%q) = %v, want %v", tc.value, got, tc.want)
			}
		})
	}
}
