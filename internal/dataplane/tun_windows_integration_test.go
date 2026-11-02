//go:build windows && integration

package dataplane

import "testing"

// Read-only SDK checks do not call Wintun or create/change any interface.
func TestWindowsTUNReadOnlyInterfaceInventory(t *testing.T) {
	api := nativeTUNAPI{}
	rows, err := api.interfaces()
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		actual, err := api.identity(row.LUID)
		if err != nil {
			t.Fatal(err)
		}
		if row.GUID != actual.GUID || row.LUID != actual.LUID || row.Name != actual.Name || adapterProof(row) != adapterProof(actual) {
			t.Fatal("inventory and exact native identity differ")
		}
	}
	t.Logf("read-only native interface identities: %d", len(rows))
}
