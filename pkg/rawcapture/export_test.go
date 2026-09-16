package rawcapture

import "sync"

// resetEnabled clears the cached Enabled()/ReplayEnabled() results so a test can
// re-evaluate the MYCASE_CAPTURE / MYCASE_REPLAY env after changing them.
// Test-only.
func resetEnabled() {
	enabledOnce = sync.Once{}
	enabled = false
	replayOnce = sync.Once{}
	replayEnabled = false
}
