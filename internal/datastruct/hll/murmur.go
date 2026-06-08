package hll

// murmurHash64A implements the 64-bit MurmurHash2 algorithm matching
// Redis's hllhash function (see redis/src/hyperloglog.c).
// seed is typically 0xadc83b19.
func murmurHash64A(data []byte, seed uint64) uint64 {
	const (
		m uint64 = 0xc6a4a7935bd1e995
		r uint64 = 47
	)

	h := seed ^ (uint64(len(data)) * m)

	// Process 8 bytes at a time.
	n := len(data) / 8
	for i := 0; i < n; i++ {
		off := i * 8
		k := uint64(data[off]) |
			uint64(data[off+1])<<8 |
			uint64(data[off+2])<<16 |
			uint64(data[off+3])<<24 |
			uint64(data[off+4])<<32 |
			uint64(data[off+5])<<40 |
			uint64(data[off+6])<<48 |
			uint64(data[off+7])<<56

		k *= m
		k ^= k >> r
		k *= m

		h ^= k
		h *= m
	}

	// Handle remaining bytes.
	tail := data[n*8:]
	switch len(tail) {
	case 7:
		h ^= uint64(tail[6]) << 48
		fallthrough
	case 6:
		h ^= uint64(tail[5]) << 40
		fallthrough
	case 5:
		h ^= uint64(tail[4]) << 32
		fallthrough
	case 4:
		h ^= uint64(tail[3]) << 24
		fallthrough
	case 3:
		h ^= uint64(tail[2]) << 16
		fallthrough
	case 2:
		h ^= uint64(tail[1]) << 8
		fallthrough
	case 1:
		h ^= uint64(tail[0])
		h *= m
	}

	h ^= h >> r
	h *= m
	h ^= h >> r

	return h
}
