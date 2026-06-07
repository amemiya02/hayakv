package rdb

import (
	"hash/crc64"

	"github.com/hdt3213/rdb/crc64jones"
)

// redisCRC64Table is built the same way crc64jones builds its internal table:
// MakeTable(reflect(Jones)). crc64jones.Update then applies the Jones
// byte-indexing that reproduces Redis's crc64 (golden vector
// crc64("123456789") == 0xe9c6d914c4b8d9ca, asserted in crc64_test.go).
var redisCRC64Table = crc64.MakeTable(reflectPoly(crc64jones.Jones))

// reflectPoly reverses the bit order of poly (identical to crc64jones.reflect,
// which is unexported there).
func reflectPoly(poly uint64) uint64 {
	x := poly & 1
	for i := 1; i < 64; i++ {
		poly >>= 1
		x <<= 1
		x |= poly & 1
	}
	return x
}

// crc64Update folds b into the running crc and returns the new value, using the
// Jones update from crc64jones (NOT stdlib crc64.Update, which differs).
func crc64Update(crc uint64, b []byte) uint64 {
	return crc64jones.Update(crc, redisCRC64Table, b)
}
