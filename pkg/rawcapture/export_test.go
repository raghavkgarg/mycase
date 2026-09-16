package rawcapture

import "sync"

// resetEnabled clears the cached Enabled() result so a test can re-evaluate the
// MYCASE_CAPTURE env after changing it. Test-only.
func resetEnabled() {
	enabledOnce = sync.Once{}
	enabled = false
}
