package object

// ObjType represents the type of a Redis object
type ObjType int

const (
	TypeString ObjType = iota
	TypeList
	TypeSet
	TypeHash
	TypeZSet
)

// Encoding represents the encoding of a Redis object
type Encoding int

const (
	EncInt       Encoding = iota // int64
	EncEmbstr                    // embedded string (<=44 bytes)
	EncRaw                       // raw string (>44 bytes)
	EncListpack                  // listpack
	EncQuicklist                 // quicklist
	EncIntset                    // intset
	EncHashtable                 // hashtable/dict
	EncSkiplist                  // skiplist (for sorted set)
)

// Robj is a Redis object with type, encoding, and pointer to actual data
type Robj struct {
	Type     ObjType
	Encoding Encoding
	Ptr      interface{}
}

// NewRobj creates a new Robj
func NewRobj(typ ObjType, enc Encoding, ptr interface{}) *Robj {
	return &Robj{
		Type:     typ,
		Encoding: enc,
		Ptr:      ptr,
	}
}

// TypeName returns the type name of the object
func (r *Robj) TypeName() string {
	switch r.Type {
	case TypeString:
		return "string"
	case TypeList:
		return "list"
	case TypeSet:
		return "set"
	case TypeHash:
		return "hash"
	case TypeZSet:
		return "zset"
	default:
		return "unknown"
	}
}

// EncodingName returns the encoding name of the object
func (r *Robj) EncodingName() string {
	switch r.Encoding {
	case EncInt:
		return "int"
	case EncEmbstr:
		return "embstr"
	case EncRaw:
		return "raw"
	case EncListpack:
		return "listpack"
	case EncQuicklist:
		return "quicklist"
	case EncIntset:
		return "intset"
	case EncHashtable:
		return "hashtable"
	case EncSkiplist:
		return "skiplist"
	default:
		return "unknown"
	}
}

// Value returns the underlying data pointer
func (r *Robj) Value() interface{} {
	return r.Ptr
}

// MakeStringObject creates a string object with the appropriate encoding
func MakeStringObject(b []byte) *Robj {
	// Try to parse as int
	if v, ok := isInt(string(b)); ok {
		return &Robj{
			Type:     TypeString,
			Encoding: EncInt,
			Ptr:      v,
		}
	}

	// Check if it's short enough for embstr (<=44 bytes)
	if len(b) <= 44 {
		return &Robj{
			Type:     TypeString,
			Encoding: EncEmbstr,
			Ptr:      b,
		}
	}

	// Otherwise, raw string
	return &Robj{
		Type:     TypeString,
		Encoding: EncRaw,
		Ptr:      b,
	}
}

// GetStringBytes returns the string value as bytes
func (r *Robj) GetStringBytes() []byte {
	switch r.Encoding {
	case EncInt:
		v := r.Ptr.(int64)
		return []byte(formatInt64(v))
	case EncEmbstr, EncRaw:
		return r.Ptr.([]byte)
	default:
		return nil
	}
}

// GetStringAsInt returns the string value as int64 if it's an integer encoding
func (r *Robj) GetStringAsInt() (int64, bool) {
	if r.Encoding == EncInt {
		return r.Ptr.(int64), true
	}
	return 0, false
}
