package admin

import (
	"math"
	"strconv"
	"strings"
)

// byteUnits are the binary units ByteSize uses after bytes, smallest first.
var byteUnits = []string{"KiB", "MiB", "GiB", "TiB", "PiB", "EiB"}

// shortHashLength is how many leading characters ShortHash keeps.
const shortHashLength = 12

// ByteSize formats a byte count with binary units and one decimal place,
// dropping a trailing ".0": 512 is "512 B", 1536 is "1.5 KiB" and 1 MiB is
// "1 MiB". A negative size is shown as "0 B". It is a presentation helper
// for changelist cells.
func ByteSize(size int64) string {
	if size < 1024 {
		if size < 0 {
			size = 0
		}
		return strconv.FormatInt(size, 10) + " B"
	}

	value := float64(size) / 1024
	unit := 0
	// Move up a unit when the value would print as 1024 of the current one.
	for math.Round(value*10)/10 >= 1024 && unit < len(byteUnits)-1 {
		value /= 1024
		unit++
	}
	text := strings.TrimSuffix(strconv.FormatFloat(value, 'f', 1, 64), ".0")
	return text + " " + byteUnits[unit]
}

// ShortHash abbreviates a hex digest (such as a SHA-256) to its first twelve
// characters, enough to recognize a file in a list. A shorter value is
// returned unchanged. It counts runes, so it never splits a character.
func ShortHash(digest string) string {
	runes := []rune(digest)
	if len(runes) <= shortHashLength {
		return digest
	}
	return string(runes[:shortHashLength])
}
