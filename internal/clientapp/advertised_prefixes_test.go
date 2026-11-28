package clientapp

import (
	"slices"
	"testing"
)

func TestAdvertisedPrefixesBelongToOneEnrollment(t *testing.T) {
	const attempt = "ab29d545-007d-4e44-b1c3-d34a6ca838ae"
	const key = "ENDLESSNET_CLIENT_ADVERTISED_CIDRS_AB29D545007D4E44B1C3D34A6CA838AE"
	t.Setenv(key, "10.20.0.0/24,0.0.0.0/0")
	got, err := advertisedPrefixes(attempt)
	if err != nil || !slices.Equal(got, []string{"0.0.0.0/0", "10.20.0.0/24"}) {
		t.Fatal(got, err)
	}
	const other = "ab29d545-007d-4e44-b1c3-d34a6ca838af"
	t.Setenv("ENDLESSNET_CLIENT_ADVERTISED_CIDRS_AB29D545007D4E44B1C3D34A6CA838AF", "")
	if got, err := advertisedPrefixes(other); err != nil || len(got) != 0 {
		t.Fatal("foreign registration", got, err)
	}
	for _, raw := range []string{"10.20.0.1/24", "10.20.0.0/24,10.20.0.0/24"} {
		t.Setenv(key, raw)
		if got, err := advertisedPrefixes(attempt); err == nil || len(got) != 0 {
			t.Fatal("invalid declaration", got, err)
		}
	}
	t.Setenv(key, "")
	if got, err := advertisedPrefixes(attempt); err != nil || len(got) != 0 {
		t.Fatal("withdrawal", got, err)
	}
}
