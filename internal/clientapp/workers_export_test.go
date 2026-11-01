package clientapp

// Healthy is a test observation of lifecycle state, not a runtime health API.
func (w *Workers) Healthy() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.running && !w.stopping && !w.failed
}
