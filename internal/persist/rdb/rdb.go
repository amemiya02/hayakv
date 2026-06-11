package rdb

import "strconv"

// Opcodes (first byte of a record at top level).
const (
	opAux       = 0xFA
	opResizeDB  = 0xFB
	opExpireMS  = 0xFC
	opExpireSec = 0xFD
	opSelectDB  = 0xFE
	opEOF       = 0xFF
	opIdle      = 0xF8
	opFreq      = 0xF9
	opModuleAux = 0xF7
	opFunction2 = 0xF5
)

// Value type bytes (hayakv reads/writes the plain 0..4 forms).
const (
	typeString = 0
	typeList   = 1
	typeSet    = 2
	typeZSet   = 3 // v1: score serialized as an ASCII string
	typeHash   = 4

	// typeStream is a private byte (was 20; moved to free RDB_TYPE_SET_LISTPACK=20).
	// Old hayakv RDBs containing streams are not forward-compatible — acceptable for
	// this learning project (no production data).
	typeStream = 200
)

// Modern Redis 8 encoding type bytes (decode-only; hayakv still writes 0-4).
const (
	typeZSet2          = 5  // RDB_TYPE_ZSET_2: score is an 8-byte binary double
	typeSetIntset      = 11 // RDB_TYPE_SET_INTSET
	typeHashListpack   = 16 // RDB_TYPE_HASH_LISTPACK
	typeZSetListpack   = 17 // RDB_TYPE_ZSET_LISTPACK
	typeListQuicklist2 = 18 // RDB_TYPE_LIST_QUICKLIST_2
	typeSetListpack    = 20 // RDB_TYPE_SET_LISTPACK (freed from the old stream byte)
)

// Length-encoding tags (top two bits of the first length byte).
const (
	len6Bit    = 0 // 00xxxxxx
	len14Bit   = 1 // 01xxxxxx
	lenSpecial = 3 // 11xxxxxx -> special (int / lzf)
	// first byte == 0x80 -> 32-bit big-endian length follows
	// first byte == 0x81 -> 64-bit big-endian length follows
	len32 = 0x80
	len64 = 0x81
)

// Special-encoding subtypes (low 6 bits when top two bits == 11).
const (
	encInt8  = 0
	encInt16 = 1
	encInt32 = 2
	encLZF   = 3
)

// Version is the highest RDB version the decoder accepts (aligned with Redis 8.x).
// The encoder writes v11 (see Encoder.WriteHeader) for compatibility with real Redis.
const Version = 12

// EntryType mirrors the value type byte for decoded entries.
type EntryType byte

// Entry is one decoded key/value record.
type Entry struct {
	DBIndex int
	Key     []byte
	Type    EntryType
	// exactly one of the following is populated based on Type:
	StringVal []byte
	ListVal   [][]byte
	SetVal    [][]byte
	HashVal   map[string][]byte
	ZSetVal   []ZSetMember
	StreamVal *StreamData
	// ExpireMS is the absolute expire in unix milliseconds; 0 means no expiry.
	ExpireMS uint64
}

// ZSetMember is one (member, score) pair in a decoded zset.
type ZSetMember struct {
	Member []byte
	Score  float64
}

// StreamData holds a decoded stream's entries, groups, and metadata.
type StreamData struct {
	LastID       StreamID
	MaxDeletedID StreamID
	EntriesAdded uint64
	Entries      []StreamEntry
	Groups       []StreamGroupData
}

// StreamID is a decoded stream entry ID.
type StreamID struct {
	Ms  uint64
	Seq uint64
}

// String returns the stream ID in "ms-seq" format.
func (id StreamID) String() string {
	return strconv.FormatUint(id.Ms, 10) + "-" + strconv.FormatUint(id.Seq, 10)
}

// StreamEntry is one decoded stream entry.
type StreamEntry struct {
	ID     StreamID
	Fields [][2]string
}

// StreamGroupData holds a decoded consumer group's state.
type StreamGroupData struct {
	Name          string
	LastDelivered StreamID
	EntriesRead   uint64
	Pending       []StreamPendingEntry
	Consumers     []StreamConsumerData
}

// StreamPendingEntry is one decoded PEL entry.
type StreamPendingEntry struct {
	ID            StreamID
	Consumer      string
	DeliveryTime  int64
	DeliveryCount uint64
}

// StreamConsumerData holds a decoded consumer's state.
type StreamConsumerData struct {
	Name       string
	ActiveTime int64
	Pending    []StreamID
}
