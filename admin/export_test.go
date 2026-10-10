package admin

import "time"

// SetPreviewTimeoutForTest shortens the preview renderer deadline for one
// test and returns the function that restores it. It is only compiled into
// the package's tests.
func SetPreviewTimeoutForTest(d time.Duration) func() {
	previous := previewTimeout
	previewTimeout = d
	return func() { previewTimeout = previous }
}
