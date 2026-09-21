package rawcapture

import "sync"

// resetState clears the cached Enabled()/ReplayEnabled() results and the wired
// Sink so a test can re-evaluate the MYCASE_CAPTURE / MYCASE_REPLAY env after
// changing them and start from a clean sink. Test-only.
func resetState() {
	enabledOnce = sync.Once{}
	enabled = false
	replayOnce = sync.Once{}
	replayEnabled = false
	sinkMu.Lock()
	sink = nil
	sinkMu.Unlock()
}
