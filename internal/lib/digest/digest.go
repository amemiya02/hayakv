// Package digest provides value digests for conditional commands (IFDEQ/IFDNE).
// Uses XXH3-128 (zeebo/xxh3) to match Redis 8.4's digest algorithm.
package digest

import (
	"encoding/binary"
	"encoding/hex"

	"github.com/zeebo/xxh3"
)

// ValueDigest returns the lowercase 32-char hex XXH3-128 digest of b.
func ValueDigest(b []byte) string {
	h := xxh3.Hash128(b)
	var buf [16]byte
	binary.BigEndian.PutUint64(buf[0:8], h.Hi)
	binary.BigEndian.PutUint64(buf[8:16], h.Lo)
	return hex.EncodeToString(buf[:])
}

// FromHex decodes a hex digest string. Returns nil if invalid.
func FromHex(s string) []byte {
	b, err := hex.DecodeString(s)
	if err != nil {
		return nil
	}
	return b
}
