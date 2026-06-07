package rdb

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
	"strconv"
)

// writer wraps an io.Writer and accumulates a CRC64 of everything written.
type writer struct {
	w   io.Writer
	crc uint64
}

func newWriter(w io.Writer) *writer { return &writer{w: w} }

func (w *writer) writeBytes(b []byte) error {
	w.crc = crc64Update(w.crc, b)
	_, err := w.w.Write(b)
	return err
}

func (w *writer) writeByte(b byte) error { return w.writeBytes([]byte{b}) }

// writeLen encodes n with the RDB length prefix (never a special form).
func (w *writer) writeLen(n uint64) error {
	switch {
	case n < (1 << 6):
		return w.writeByte(byte(len6Bit<<6) | byte(n))
	case n < (1 << 14):
		return w.writeBytes([]byte{byte(len14Bit<<6) | byte(n>>8), byte(n)})
	case n <= 0xFFFFFFFF:
		var b [5]byte
		b[0] = len32
		binary.BigEndian.PutUint32(b[1:], uint32(n))
		return w.writeBytes(b[:])
	default:
		var b [9]byte
		b[0] = len64
		binary.BigEndian.PutUint64(b[1:], n)
		return w.writeBytes(b[:])
	}
}

// writeString encodes s, preferring the compact integer form when s is a
// canonical int64 literal that round-trips, else a length-prefixed raw string.
func (w *writer) writeString(s []byte) error {
	if iv, ok := tryParseInt(s); ok {
		return w.writeIntString(iv)
	}
	if err := w.writeLen(uint64(len(s))); err != nil {
		return err
	}
	return w.writeBytes(s)
}

func (w *writer) writeIntString(v int64) error {
	switch {
	case v >= -(1<<7) && v < (1<<7):
		return w.writeBytes([]byte{byte(lenSpecial<<6) | encInt8, byte(int8(v))})
	case v >= -(1<<15) && v < (1<<15):
		var b [3]byte
		b[0] = byte(lenSpecial<<6) | encInt16
		binary.LittleEndian.PutUint16(b[1:], uint16(int16(v)))
		return w.writeBytes(b[:])
	default:
		var b [5]byte
		b[0] = byte(lenSpecial<<6) | encInt32
		binary.LittleEndian.PutUint32(b[1:], uint32(int32(v)))
		return w.writeBytes(b[:])
	}
}

// formatScore serializes a zset score the way the RDB v1 zset type expects:
// the shortest decimal that round-trips, matching strconv's 'g' default that
// hdt3213/rdb and redis both accept on load.
func formatScore(f float64) []byte {
	return []byte(strconv.FormatFloat(f, 'g', -1, 64))
}

// writeRawString writes s as a raw length-prefixed string WITHOUT the compact
// integer optimisation that writeString applies.  Used for ZSet scores, which
// real Redis encodes as length + ASCII double (never as int8/16/32 special
// form).  Using writeString for scores like 1 would produce 0xC0 0x01 (int8
// encoding); real Redis writes 0x01 0x31 (length 1 + "1").
func (w *writer) writeRawString(s []byte) error {
	if err := w.writeLen(uint64(len(s))); err != nil {
		return err
	}
	return w.writeBytes(s)
}

// tryParseInt reports whether s is a canonical int32-range integer literal
// (canonical = strconv.FormatInt produces the same bytes, so it round-trips).
func tryParseInt(s []byte) (int64, bool) {
	if len(s) == 0 || len(s) > 11 {
		return 0, false
	}
	v, err := strconv.ParseInt(string(s), 10, 64)
	if err != nil {
		return 0, false
	}
	if v < -(1<<31) || v >= (1<<31) {
		return 0, false // only int8/16/32 special forms are emitted
	}
	if strconv.FormatInt(v, 10) != string(s) {
		return 0, false // leading zeros / "+1" etc. must stay raw
	}
	return v, true
}

// reader wraps a buffered reader and tracks bytes consumed for CRC validation.
type reader struct {
	r   *bufio.Reader
	crc uint64
}

func newReader(r io.Reader) *reader { return &reader{r: bufio.NewReader(r)} }

func (r *reader) readByte() (byte, error) {
	b, err := r.r.ReadByte()
	if err == nil {
		r.crc = crc64Update(r.crc, []byte{b})
	}
	return b, err
}

func (r *reader) readFull(n int) ([]byte, error) {
	buf := make([]byte, n)
	if _, err := io.ReadFull(r.r, buf); err != nil {
		return nil, err
	}
	r.crc = crc64Update(r.crc, buf)
	return buf, nil
}

// readLen decodes a length prefix. special=true means the caller hit a 11xxxxxx
// special encoding; the low 6 bits are returned in n for the caller to dispatch.
func (r *reader) readLen() (n uint64, special bool, err error) {
	first, err := r.readByte()
	if err != nil {
		return 0, false, err
	}
	switch first >> 6 {
	case len6Bit:
		return uint64(first & 0x3F), false, nil
	case len14Bit:
		second, err := r.readByte()
		if err != nil {
			return 0, false, err
		}
		return uint64(first&0x3F)<<8 | uint64(second), false, nil
	case lenSpecial:
		return uint64(first & 0x3F), true, nil
	default: // first byte is exactly 0x80 or 0x81
		if first == len32 {
			b, err := r.readFull(4)
			if err != nil {
				return 0, false, err
			}
			return uint64(binary.BigEndian.Uint32(b)), false, nil
		}
		if first == len64 {
			b, err := r.readFull(8)
			if err != nil {
				return 0, false, err
			}
			return binary.BigEndian.Uint64(b), false, nil
		}
		return 0, false, fmt.Errorf("rdb: bad length byte %#x", first)
	}
}

// readString decodes a length-prefixed or special-int string.
func (r *reader) readString() ([]byte, error) {
	n, special, err := r.readLen()
	if err != nil {
		return nil, err
	}
	if !special {
		return r.readFull(int(n))
	}
	switch n {
	case encInt8:
		b, err := r.readFull(1)
		if err != nil {
			return nil, err
		}
		return []byte(strconv.FormatInt(int64(int8(b[0])), 10)), nil
	case encInt16:
		b, err := r.readFull(2)
		if err != nil {
			return nil, err
		}
		return []byte(strconv.FormatInt(int64(int16(binary.LittleEndian.Uint16(b))), 10)), nil
	case encInt32:
		b, err := r.readFull(4)
		if err != nil {
			return nil, err
		}
		return []byte(strconv.FormatInt(int64(int32(binary.LittleEndian.Uint32(b))), 10)), nil
	case encLZF:
		return nil, fmt.Errorf("rdb: LZF-compressed strings are not supported in M6")
	default:
		return nil, fmt.Errorf("rdb: unknown special string encoding %d", n)
	}
}
