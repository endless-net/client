package client

import "testing"

// Call only after the previous worker has stopped. Forget this test's store,
// not the global registry, so the replacement runtime must decode durable bytes.
func reopenRPCStoreFromDisk(t *testing.T, previous *ConfigStore) *ConfigStore {
	t.Helper()
	path := previous.path
	configStores.Delete(path)
	t.Cleanup(func() { configStores.Delete(path) })
	store, err := OpenConfigStore(path)
	if err != nil {
		t.Fatal("cannot reopen durable RPC state", err)
	}
	if store == previous {
		t.Fatal("restart reused process-local RPC state")
	}
	return store
}
