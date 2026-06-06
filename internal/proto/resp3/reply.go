package resp3

import (
	"bytes"
	"math"
	"strconv"

	iredis "github.com/amemiya02/hayakv/internal/iface/redis"
)

const CRLF = "\r\n"

// NullReply is the RESP3 null type (_\r\n)
type NullReply struct{}

func MakeNullReply() *NullReply      { return &NullReply{} }
func (r *NullReply) ToBytes() []byte { return []byte("_\r\n") }

// BoolReply is the RESP3 boolean type (#t\r\n or #f\r\n)
type BoolReply struct{ Val bool }

func MakeBoolReply(v bool) *BoolReply { return &BoolReply{Val: v} }
func (r *BoolReply) ToBytes() []byte {
	if r.Val {
		return []byte("#t\r\n")
	}
	return []byte("#f\r\n")
}

// DoubleReply is the RESP3 double type (,3.14\r\n)
type DoubleReply struct{ Val float64 }

func MakeDoubleReply(v float64) *DoubleReply { return &DoubleReply{Val: v} }
func (r *DoubleReply) ToBytes() []byte {
	var s string
	switch {
	case math.IsInf(r.Val, 1):
		s = "inf"
	case math.IsInf(r.Val, -1):
		s = "-inf"
	case math.IsNaN(r.Val):
		s = "nan"
	default:
		s = strconv.FormatFloat(r.Val, 'f', -1, 64)
	}
	return []byte("," + s + CRLF)
}

// BigNumberReply is the RESP3 bignum type ((12345\r\n)
type BigNumberReply struct{ Val string }

func MakeBigNumberReply(v string) *BigNumberReply { return &BigNumberReply{Val: v} }
func (r *BigNumberReply) ToBytes() []byte         { return []byte("(" + r.Val + CRLF) }

// MapReply is the RESP3 map type (%N\r\n followed by N key-value pairs)
// Pairs is an even-length slice: [key0, val0, key1, val1, ...]
type MapReply struct{ Pairs []iredis.Reply }

func MakeMapReply(pairs []iredis.Reply) *MapReply { return &MapReply{Pairs: pairs} }
func (r *MapReply) ToBytes() []byte {
	var b bytes.Buffer
	b.WriteString("%" + strconv.Itoa(len(r.Pairs)/2) + CRLF)
	for _, p := range r.Pairs {
		b.Write(p.ToBytes())
	}
	return b.Bytes()
}

// SetReply is the RESP3 set type (~N\r\n followed by N members)
type SetReply struct{ Members []iredis.Reply }

func MakeSetReply(m []iredis.Reply) *SetReply { return &SetReply{Members: m} }
func (r *SetReply) ToBytes() []byte {
	var b bytes.Buffer
	b.WriteString("~" + strconv.Itoa(len(r.Members)) + CRLF)
	for _, m := range r.Members {
		b.Write(m.ToBytes())
	}
	return b.Bytes()
}

// PushReply is the RESP3 push type (>N\r\n followed by N elements)
type PushReply struct{ Elems []iredis.Reply }

func MakePushReply(e []iredis.Reply) *PushReply { return &PushReply{Elems: e} }
func (r *PushReply) ToBytes() []byte {
	var b bytes.Buffer
	b.WriteString(">" + strconv.Itoa(len(r.Elems)) + CRLF)
	for _, e := range r.Elems {
		b.Write(e.ToBytes())
	}
	return b.Bytes()
}

// VerbatimReply is the RESP3 verbatim string type (=N\r\nformat:text\r\n)
type VerbatimReply struct {
	Format string // "txt" | "mkd"
	Text   string
}

func MakeVerbatimReply(format, text string) *VerbatimReply {
	return &VerbatimReply{Format: format, Text: text}
}
func (r *VerbatimReply) ToBytes() []byte {
	body := r.Format + ":" + r.Text
	return []byte("=" + strconv.Itoa(len(body)) + CRLF + body + CRLF)
}
