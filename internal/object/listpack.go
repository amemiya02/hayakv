package object

import (
	"encoding/binary"
	"math"
)

// Tag bytes for listpack element types
const (
	elemStr byte = 0x00
	elemInt byte = 0x01
)

// encodeStrElem encodes a string element into bytes
// Format: tag(0x00) + uvarint(length) + bytes
func encodeStrElem(s string) []byte {
	buf := make([]byte, 0, 1+binary.MaxVarintLen64+len(s))
	buf = append(buf, elemStr)
	buf = binary.AppendUvarint(buf, uint64(len(s)))
	buf = append(buf, s...)
	return buf
}

// encodeIntElem encodes an int64 element into bytes
// Format: tag(0x01) + zigzag varint
func encodeIntElem(v int64) []byte {
	buf := make([]byte, 0, 1+binary.MaxVarintLen64)
	buf = append(buf, elemInt)
	buf = binary.AppendVarint(buf, v)
	return buf
}

// decodeElem decodes an element from bytes, returning the value and bytes consumed
// Returns: value (string or int64), bytes consumed, error
func decodeElem(data []byte) (interface{}, int, error) {
	if len(data) == 0 {
		return nil, 0, nil
	}

	tag := data[0]
	pos := 1

	switch tag {
	case elemStr:
		// Read uvarint length
		strLen, n := binary.Uvarint(data[pos:])
		if n <= 0 {
			return nil, 0, nil
		}
		pos += n

		// Read string bytes
		if pos+int(strLen) > len(data) {
			return nil, 0, nil
		}
		s := string(data[pos : pos+int(strLen)])
		return s, pos + int(strLen), nil

	case elemInt:
		// Read zigzag varint
		v, n := binary.Varint(data[pos:])
		if n <= 0 {
			return nil, 0, nil
		}
		pos += n
		return v, pos, nil

	default:
		return nil, 0, nil
	}
}

// isInt checks if a string can be converted to int64 with round-trip
func isInt(s string) (int64, bool) {
	if len(s) == 0 {
		return 0, false
	}

	// Parse as int64
	v, err := parseInt64(s)
	if err != nil {
		return 0, false
	}

	// Round-trip check
	roundTrip := formatInt64(v)
	return v, roundTrip == s
}

// parseInt64 parses a string as int64
func parseInt64(s string) (int64, error) {
	if len(s) == 0 {
		return 0, nil
	}

	negative := false
	start := 0
	if s[0] == '-' {
		negative = true
		start = 1
	} else if s[0] == '+' {
		start = 1
	}

	var v int64
	for i := start; i < len(s); i++ {
		c := s[i]
		if c < '0' || c > '9' {
			return 0, nil
		}
		digit := int64(c - '0')
		if v > (math.MaxInt64-digit)/10 {
			return 0, nil
		}
		v = v*10 + digit
	}

	if negative {
		v = -v
	}
	return v, nil
}

// formatInt64 formats an int64 as string
func formatInt64(v int64) string {
	if v == 0 {
		return "0"
	}

	negative := false
	if v < 0 {
		negative = true
		v = -v
	}

	// Count digits
	digits := 0
	tmp := v
	for tmp > 0 {
		digits++
		tmp /= 10
	}

	// Build string
	buf := make([]byte, digits)
	for i := digits - 1; i >= 0; i-- {
		buf[i] = byte('0' + v%10)
		v /= 10
	}

	if negative {
		return "-" + string(buf)
	}
	return string(buf)
}

// Listpack is a compact list of elements stored in a single byte slice
type Listpack struct {
	buf []byte
	num int
}

// NewListpack creates a new empty listpack
func NewListpack() *Listpack {
	return &Listpack{
		buf: make([]byte, 0),
		num: 0,
	}
}

// Len returns the number of elements in the listpack
func (lp *Listpack) Len() int {
	return lp.num
}

// Bytes returns the underlying byte slice
func (lp *Listpack) Bytes() []byte {
	return lp.buf
}

// AppendStr appends a string element to the listpack
func (lp *Listpack) AppendStr(s string) {
	lp.buf = append(lp.buf, encodeStrElem(s)...)
	lp.num++
}

// AppendInt appends an int64 element to the listpack
func (lp *Listpack) AppendInt(v int64) {
	lp.buf = append(lp.buf, encodeIntElem(v)...)
	lp.num++
}

// Get returns the element at the given index
func (lp *Listpack) Get(i int) (interface{}, bool) {
	if i < 0 || i >= lp.num {
		return nil, false
	}

	pos := 0
	for j := 0; j < i; j++ {
		_, consumed, _ := decodeElem(lp.buf[pos:])
		if consumed == 0 {
			return nil, false
		}
		pos += consumed
	}

	val, _, _ := decodeElem(lp.buf[pos:])
	return val, val != nil
}

// Range iterates over all elements, calling fn for each
// If fn returns false, iteration stops
func (lp *Listpack) Range(fn func(i int, val interface{}) bool) {
	pos := 0
	for i := 0; i < lp.num; i++ {
		val, consumed, _ := decodeElem(lp.buf[pos:])
		if consumed == 0 {
			break
		}
		if !fn(i, val) {
			break
		}
		pos += consumed
	}
}
