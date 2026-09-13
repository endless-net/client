package testclient

// observeAgentCompletion is used only by the synchronous readiness failure path.
// The buffered completion remains available to Stop/Crash; the observation never
// waits for the process, reads its output, or replaces the readiness predicate.
func observeAgentCompletion(done chan error) (completed, failed bool) {
	select {
	case err := <-done:
		done <- err
		return true, err != nil
	default:
		return false, false
	}
}
