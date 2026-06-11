package rdb

import (
	"encoding/binary"
	"fmt"
	"strconv"
)

// parseListpack decodes a Redis listpack blob into its element sequence.
// Integer elements are rendered as decimal ASCII, matching readString. The
// blob is untrusted: every bounds violation returns an error, never panics.
func parseListpack(blob []byte) ([][]byte, error) {
	if len(blob) < 7 { // 6-byte header + at least the 0xFF terminator
		return nil, fmt.Errorf("rdb: listpack too short (%d bytes)", len(blob))
	}
	total := binary.LittleEndian.Uint32(blob[0:4])
	if int(total) != len(blob) {
		return nil, fmt.Errorf("rdb: listpack total-bytes %d != blob len %d", total, len(blob))
	}
	var out [][]byte
	p := 6 // skip 4-byte total + 2-byte num-elements
	for p < len(blob) {
		b0 := blob[p]
		if b0 == 0xFF { // terminator
			return out, nil
		}
		var val []byte
		var encDataLen int
		switch {
		case b0 < 0x80: // 0xxxxxxx: 7-bit unsigned int
			val = []byte(strconv.FormatInt(int64(b0&0x7F), 10))
			encDataLen = 1
		case b0 < 0xC0: // 10xxxxxx: 6-bit string length
			l := int(b0 & 0x3F)
			if p+1+l > len(blob) {
				return nil, fmt.Errorf("rdb: listpack 6-bit string overflow")
			}
			val = blob[p+1 : p+1+l]
			encDataLen = 1 + l
		case b0 < 0xE0: // 110xxxxx: 13-bit signed int
			if p+2 > len(blob) {
				return nil, fmt.Errorf("rdb: listpack 13-bit int overflow")
			}
			v := int64(b0&0x1F)<<8 | int64(blob[p+1])
			if v >= (1 << 12) {
				v -= 1 << 13
			}
			val = []byte(strconv.FormatInt(v, 10))
			encDataLen = 2
		case b0 < 0xF0: // 1110xxxx: 12-bit string length
			if p+2 > len(blob) {
				return nil, fmt.Errorf("rdb: listpack 12-bit string header overflow")
			}
			l := int(b0&0x0F)<<8 | int(blob[p+1])
			if p+2+l > len(blob) {
				return nil, fmt.Errorf("rdb: listpack 12-bit string overflow")
			}
			val = blob[p+2 : p+2+l]
			encDataLen = 2 + l
		case b0 == 0xF0: // 32-bit string length
			if p+5 > len(blob) {
				return nil, fmt.Errorf("rdb: listpack 32-bit string header overflow")
			}
			l := int(binary.LittleEndian.Uint32(blob[p+1 : p+5]))
			if l < 0 || p+5+l > len(blob) {
				return nil, fmt.Errorf("rdb: listpack 32-bit string overflow")
			}
			val = blob[p+5 : p+5+l]
			encDataLen = 5 + l
		case b0 == 0xF1: // int16
			if p+3 > len(blob) {
				return nil, fmt.Errorf("rdb: listpack int16 overflow")
			}
			v := int16(binary.LittleEndian.Uint16(blob[p+1 : p+3]))
			val = []byte(strconv.FormatInt(int64(v), 10))
			encDataLen = 3
		case b0 == 0xF2: // int24
			if p+4 > len(blob) {
				return nil, fmt.Errorf("rdb: listpack int24 overflow")
			}
			u := uint32(blob[p+1]) | uint32(blob[p+2])<<8 | uint32(blob[p+3])<<16
			v := int64(u)
			if v >= (1 << 23) {
				v -= 1 << 24
			}
			val = []byte(strconv.FormatInt(v, 10))
			encDataLen = 4
		case b0 == 0xF3: // int32
			if p+5 > len(blob) {
				return nil, fmt.Errorf("rdb: listpack int32 overflow")
			}
			v := int32(binary.LittleEndian.Uint32(blob[p+1 : p+5]))
			val = []byte(strconv.FormatInt(int64(v), 10))
			encDataLen = 5
		case b0 == 0xF4: // int64
			if p+9 > len(blob) {
				return nil, fmt.Errorf("rdb: listpack int64 overflow")
			}
			v := int64(binary.LittleEndian.Uint64(blob[p+1 : p+9]))
			val = []byte(strconv.FormatInt(v, 10))
			encDataLen = 9
		default: // 0xF5-0xFE reserved/unused
			return nil, fmt.Errorf("rdb: listpack bad element header %#x", b0)
		}
		out = append(out, val)
		p += encDataLen + backlenSize(encDataLen)
	}
	return nil, fmt.Errorf("rdb: listpack missing terminator")
}

// backlenSize returns the number of bytes the listpack back-length field
// occupies for an element whose encoding+data spans entryLen bytes.
func backlenSize(entryLen int) int {
	switch {
	case entryLen <= 127:
		return 1
	case entryLen < 16384:
		return 2
	case entryLen < 2097152:
		return 3
	case entryLen < 268435456:
		return 4
	default:
		return 5
	}
}

// parseIntset decodes a Redis intset blob into decimal-ASCII members.
// Layout: 4-byte encoding LE (2|4|8), 4-byte length LE, then length signed
// little-endian integers of `encoding` bytes each.
func parseIntset(blob []byte) ([][]byte, error) {
	if len(blob) < 8 {
		return nil, fmt.Errorf("rdb: intset too short (%d bytes)", len(blob))
	}
	enc := binary.LittleEndian.Uint32(blob[0:4])
	length := binary.LittleEndian.Uint32(blob[4:8])
	if enc != 2 && enc != 4 && enc != 8 {
		return nil, fmt.Errorf("rdb: intset bad encoding %d", enc)
	}
	need := 8 + int(length)*int(enc)
	if need > len(blob) {
		return nil, fmt.Errorf("rdb: intset length %d*%d overflows blob %d", length, enc, len(blob))
	}
	out := make([][]byte, 0, length)
	p := 8
	for i := uint32(0); i < length; i++ {
		var v int64
		switch enc {
		case 2:
			v = int64(int16(binary.LittleEndian.Uint16(blob[p : p+2])))
		case 4:
			v = int64(int32(binary.LittleEndian.Uint32(blob[p : p+4])))
		case 8:
			v = int64(binary.LittleEndian.Uint64(blob[p : p+8]))
		}
		out = append(out, []byte(strconv.FormatInt(v, 10)))
		p += int(enc)
	}
	return out, nil
}
