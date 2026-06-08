// Package digest provides value digests for conditional commands (IFDEQ/IFDNE).
// Uses XXH64 (cespare/xxhash/v2). Redis 8.4 uses XXH3-128; the hex length
// differs but the semantics (stable, fast, non-cryptographic) are the same.
// If exact parity is needed, swap the hash implementation here.
package digest

import (
	"encoding/hex"
	"fmt"

	"github.com/cespare/xxhash/v2"
)

// ValueDigest returns the lowercase hex digest of b.
func ValueDigest(b []byte) string {
	h := xxhash.Sum64(b)
	// xxhash.Sum64 returns 8 bytes; format as 16-char hex
	return fmt.Sprintf("%016x", h)
}

// ValueDigestBytes is like ValueDigest but returns raw bytes (8 bytes, big-endian).
func ValueDigestBytes(b []byte) []byte {
	h := xxhash.Sum64(b)
	buf := make([]byte, 8)
	for i := 7; i >= 0; i-- {
		buf[i] = byte(h)
		h >>= 8
	}
	return buf
}

// FromHex decodes a hex digest string. Returns nil if invalid.
func FromHex(s string) []byte {
	b, err := hex.DecodeString(s)
	if err != nil {
		return nil
	}
	return b
}
