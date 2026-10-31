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

// TokenPaletteForTest exposes the colour tokens so tests in admin_test can
// check the served stylesheet against them.
var TokenPaletteForTest = palette

// SealFlashForTest mints a flash cookie value for sessionToken without the
// catalog check setFlash applies, so tests can present a genuinely signed
// value that names an outcome the admin never sets.
var SealFlashForTest = sealFlash
