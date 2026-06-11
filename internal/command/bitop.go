package database

import (
	"strings"

	"github.com/amemiya02/hayakv/internal/datastruct/bitmap"
	"github.com/amemiya02/hayakv/internal/iface/database"
	"github.com/amemiya02/hayakv/internal/iface/redis"
	"github.com/amemiya02/hayakv/internal/lib/utils"
	"github.com/amemiya02/hayakv/internal/object"
	"github.com/amemiya02/hayakv/internal/proto/resp2/protocol"
)

func prepareBitOp(args [][]byte) ([]string, []string) {
	// BITOP op dest src1 src2 ...
	// dest is args[1], sources are args[2:]
	if len(args) < 2 {
		return nil, nil
	}
	dest := string(args[1])
	return []string{dest}, nil
}

func execBitOp(db *DB, args [][]byte) redis.Reply {
	if len(args) < 3 {
		return protocol.MakeArgNumErrReply("bitop")
	}

	op := strings.ToUpper(string(args[0]))
	dest := string(args[1])
	srcKeys := args[2:]

	switch op {
	case "AND", "OR", "XOR", "NOT", "DIFF", "DIFF1", "ANDOR", "ONE":
		// valid
	default:
		return protocol.MakeErrReply("ERR syntax error")
	}

	if op == "NOT" && len(srcKeys) != 1 {
		return protocol.MakeErrReply("ERR BITOP NOT requires exactly one source key")
	}
	if len(srcKeys) < 1 {
		return protocol.MakeArgNumErrReply("bitop")
	}

	// Load source bitmaps
	srcBitmaps := make([]*bitmap.BitMap, len(srcKeys))
	for i, key := range srcKeys {
		bs, err := db.getAsString(string(key))
		if err != nil {
			return err
		}
		if bs == nil {
			srcBitmaps[i] = bitmap.FromBytes(nil)
		} else {
			srcBitmaps[i] = bitmap.FromBytes(bs)
		}
	}

	var result *bitmap.BitMap

	switch op {
	case "AND":
		result = bitopAnd(srcBitmaps)
	case "OR":
		result = bitopOr(srcBitmaps)
	case "XOR":
		result = bitopXor(srcBitmaps)
	case "NOT":
		result = bitopNot(srcBitmaps[0])
	case "DIFF":
		result = bitopDiff(srcBitmaps)
	case "DIFF1":
		result = bitopDiff1(srcBitmaps)
	case "ANDOR":
		result = bitopAndOr(srcBitmaps)
	case "ONE":
		result = bitopOne(srcBitmaps)
	}

	resultBytes := result.ToBytes()

	db.PutEntity(dest, &database.DataEntity{Data: object.MakeRawStringObject(resultBytes)})
	db.addAof(utils.ToCmdLine3("bitop", args...))
	return protocol.MakeIntReply(int64(len(resultBytes)))
}

// maxLen returns the maximum length among all byte slices.
func maxLen(slices [][]byte) int {
	max := 0
	for _, s := range slices {
		if len(s) > max {
			max = len(s)
		}
	}
	return max
}

// getByte returns the byte at position i, or def if i is out of range.
func getByte(b []byte, i int, def byte) byte {
	if i < len(b) {
		return b[i]
	}
	return def
}

// bitopAnd performs AND across all sources. Shorter sources pad with 0xFF (identity for AND).
func bitopAnd(srcs []*bitmap.BitMap) *bitmap.BitMap {
	raw := make([][]byte, len(srcs))
	for i, s := range srcs {
		raw[i] = s.ToBytes()
	}
	length := maxLen(raw)
	result := make([]byte, length)
	for i := 0; i < length; i++ {
		b := byte(0xFF)
		for _, src := range raw {
			b &= getByte(src, i, 0xFF)
		}
		result[i] = b
	}
	return bitmap.FromBytes(result)
}

// bitopOr performs OR across all sources. Shorter sources pad with 0x00 (identity for OR).
func bitopOr(srcs []*bitmap.BitMap) *bitmap.BitMap {
	raw := make([][]byte, len(srcs))
	for i, s := range srcs {
		raw[i] = s.ToBytes()
	}
	length := maxLen(raw)
	result := make([]byte, length)
	for i := 0; i < length; i++ {
		b := byte(0x00)
		for _, src := range raw {
			b |= getByte(src, i, 0x00)
		}
		result[i] = b
	}
	return bitmap.FromBytes(result)
}

// bitopXor performs XOR across all sources. Shorter sources pad with 0x00.
func bitopXor(srcs []*bitmap.BitMap) *bitmap.BitMap {
	raw := make([][]byte, len(srcs))
	for i, s := range srcs {
		raw[i] = s.ToBytes()
	}
	length := maxLen(raw)
	result := make([]byte, length)
	for i := 0; i < length; i++ {
		b := byte(0x00)
		for _, src := range raw {
			b ^= getByte(src, i, 0x00)
		}
		result[i] = b
	}
	return bitmap.FromBytes(result)
}

// bitopNot flips all bits. Result length equals source length.
func bitopNot(src *bitmap.BitMap) *bitmap.BitMap {
	raw := src.ToBytes()
	result := make([]byte, len(raw))
	for i, b := range raw {
		result[i] = ^b
	}
	return bitmap.FromBytes(result)
}

// bitopDiff computes bits set in the first key but NOT in any other key.
// = src[0] AND NOT(src[1] OR src[2] OR ...)
func bitopDiff(srcs []*bitmap.BitMap) *bitmap.BitMap {
	if len(srcs) == 1 {
		return bitopNot(srcs[0])
	}
	// Compute OR of all sources except the first
	others := srcs[1:]
	othersOr := bitopOr(others)
	// NOT the OR
	notOr := bitopNot(othersOr)
	// AND with the first source
	return bitopAnd([]*bitmap.BitMap{srcs[0], notOr})
}

// bitopDiff1 computes bits NOT in the first key but set in at least one other.
// = NOT(src[0]) AND (src[1] OR src[2] OR ...)
func bitopDiff1(srcs []*bitmap.BitMap) *bitmap.BitMap {
	if len(srcs) == 1 {
		return bitmap.FromBytes(nil)
	}
	notFirst := bitopNot(srcs[0])
	othersOr := bitopOr(srcs[1:])
	return bitopAnd([]*bitmap.BitMap{notFirst, othersOr})
}

// bitopAndOr computes src[0] AND (src[1] OR src[2] OR ...).
func bitopAndOr(srcs []*bitmap.BitMap) *bitmap.BitMap {
	if len(srcs) == 1 {
		return srcs[0]
	}
	othersOr := bitopOr(srcs[1:])
	return bitopAnd([]*bitmap.BitMap{srcs[0], othersOr})
}

// bitopOne computes bits set in exactly one source (popcount == 1 across all sources).
func bitopOne(srcs []*bitmap.BitMap) *bitmap.BitMap {
	raw := make([][]byte, len(srcs))
	for i, s := range srcs {
		raw[i] = s.ToBytes()
	}
	length := maxLen(raw)
	result := make([]byte, length)
	for i := 0; i < length; i++ {
		// For each bit position, check if exactly one source has it set
		var out byte
		for bit := 0; bit < 8; bit++ {
			mask := byte(1 << bit)
			count := byte(0)
			for _, src := range raw {
				if getByte(src, i, 0x00)&mask != 0 {
					count++
				}
			}
			if count == 1 {
				out |= mask
			}
		}
		result[i] = out
	}
	return bitmap.FromBytes(result)
}

func init() {
	registerCommand("BitOp", execBitOp, prepareBitOp, nil, -4, flagWrite).
		attachCommandExtra([]string{redisFlagWrite, redisFlagDenyOOM}, 2, -1, 1).
		attachNotify(notifyString, "set")
}
