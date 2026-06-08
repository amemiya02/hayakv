// Package digest provides value digests for conditional commands (IFDEQ/IFDNE).
// Uses XXH3-64 (zeebo/xxh3) producing 16 hex chars, matching Redis 8.4.
package digest

import (
	"encoding/hex"
	"fmt"

	"github.com/zeebo/xxh3"
)

// ValueDigest returns the lowercase 16-char hex XXH3-64 digest of b.
func ValueDigest(b []byte) string {
	h := xxh3.Hash(b)
	return fmt.Sprintf("%016x", h)
}

// FromHex decodes a hex digest string. Returns nil if invalid.
func FromHex(s string) []byte {
	b, err := hex.DecodeString(s)
	if err != nil {
		return nil
	}
	return b
}
