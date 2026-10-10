package storage

import (
	"crypto/rand"
	"encoding/base32"
	"strings"
)

const (
	// keyBytes is the randomness in a key: 160 bits.
	keyBytes = 20
	// keyLength is the length of a key: keyBytes in unpadded base32.
	keyLength = 32
)

var keyEncoding = base32.StdEncoding.WithPadding(base32.NoPadding)

// NewKey returns a new object key: 160 random bits as 32 lowercase base32
// characters. A key is opaque. It never carries a filename or an extension,
// and it is not an authorization decision.
func NewKey() string {
	var raw [keyBytes]byte
	if _, err := rand.Read(raw[:]); err != nil {
		panic("storage: no randomness: " + err.Error())
	}
	return strings.ToLower(keyEncoding.EncodeToString(raw[:]))
}

// ValidKey reports whether key has the form NewKey produces. Every Store
// refuses any other string with ErrInvalidKey before it touches storage, so
// a key can never name a path.
func ValidKey(key string) bool {
	if len(key) != keyLength {
		return false
	}
	for i := 0; i < len(key); i++ {
		c := key[i]
		if (c < 'a' || c > 'z') && (c < '2' || c > '7') {
			return false
		}
	}
	return true
}
