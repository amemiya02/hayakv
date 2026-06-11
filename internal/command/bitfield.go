package database

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/amemiya02/hayakv/internal/datastruct/bitmap"
	"github.com/amemiya02/hayakv/internal/iface/database"
	"github.com/amemiya02/hayakv/internal/iface/redis"
	"github.com/amemiya02/hayakv/internal/lib/utils"
	"github.com/amemiya02/hayakv/internal/object"
	"github.com/amemiya02/hayakv/internal/proto/resp2/protocol"
)

// parseBitfieldType parses "i<bits>" or "u<bits>" and returns (bits, signed).
func parseBitfieldType(s string) (bits int, signed bool, err error) {
	if len(s) < 2 {
		return 0, false, fmt.Errorf("invalid type specifier")
	}
	prefix := s[0]
	if prefix != 'i' && prefix != 'u' {
		return 0, false, fmt.Errorf("invalid type specifier")
	}
	signed = prefix == 'i'
	bits, err = strconv.Atoi(s[1:])
	if err != nil {
		return 0, false, err
	}
	if signed {
		if bits < 1 || bits > 64 {
			return 0, false, fmt.Errorf("invalid type specifier")
		}
	} else {
		if bits < 1 || bits > 63 {
			return 0, false, fmt.Errorf("invalid type specifier")
		}
	}
	return bits, signed, nil
}

// parseBitfieldOffset parses an offset specifier. "#N" means N*bits, plain N means bit offset.
func parseBitfieldOffset(s string, bits int) (int64, error) {
	if strings.HasPrefix(s, "#") {
		n, err := strconv.ParseInt(s[1:], 10, 64)
		if err != nil {
			return 0, err
		}
		return n * int64(bits), nil
	}
	return strconv.ParseInt(s, 10, 64)
}

// getOrInitBitmap retrieves the bitmap for a key, creating it if absent.
func (db *DB) getOrInitBitmap(key string) (*bitmap.BitMap, bool, protocol.ErrorReply) {
	entity, ok := db.GetEntity(key)
	if !ok {
		bm := bitmap.New()
		return bm, true, nil
	}
	switch v := entity.Data.(type) {
	case *object.Robj:
		if v.Type != object.TypeString {
			return nil, false, &protocol.WrongTypeErrReply{}
		}
		bm := bitmap.FromBytes(v.GetStringBytes())
		return bm, false, nil
	default:
		return nil, false, &protocol.WrongTypeErrReply{}
	}
}

// bfGet reads a multi-bit field from the bitmap.
func bfGet(bm *bitmap.BitMap, offset int64, bits int, signed bool) int64 {
	var val int64
	for i := 0; i < bits; i++ {
		if bm.GetBit(offset+int64(i)) != 0 {
			val |= 1 << i
		}
	}
	// sign-extend if signed and negative
	if signed && bits < 64 && (val&(1<<(bits-1))) != 0 {
		val |= -1 << bits
	}
	return val
}

// bfSet writes a multi-bit field to the bitmap, returning the old value.
func bfSet(bm *bitmap.BitMap, offset int64, bits int, signed bool, val int64) int64 {
	old := bfGet(bm, offset, bits, signed)
	mask := int64((1 << bits) - 1)
	val &= mask
	for i := 0; i < bits; i++ {
		if val&(1<<i) != 0 {
			bm.SetBit(offset+int64(i), 1)
		} else {
			bm.SetBit(offset+int64(i), 0)
		}
	}
	return old
}

// bfIncrby increments a multi-bit field, handling overflow.
// Returns (newValue, overflowed).
func bfIncrby(bm *bitmap.BitMap, offset int64, bits int, signed bool, incr int64, overflow string) (int64, bool) {
	old := bfGet(bm, offset, bits, signed)
	newVal := old + incr

	if overflow == "" || overflow == "wrap" {
		// two's complement wrap — handled by masking
		mask := int64((1 << bits) - 1)
		newVal &= mask
		// re-sign-extend if signed
		if signed && bits < 64 && (newVal&(1<<(bits-1))) != 0 {
			newVal |= -1 << bits
		}
		bfSet(bm, offset, bits, signed, newVal)
		return newVal, false
	}

	if overflow == "sat" {
		var minVal, maxVal int64
		if signed {
			minVal = -(1 << (bits - 1))
			maxVal = (1 << (bits - 1)) - 1
		} else {
			minVal = 0
			maxVal = (1 << bits) - 1
		}
		if signed {
			if (incr > 0 && old > maxVal-incr) || (incr < 0 && old < minVal-incr) {
				// saturate
				if incr > 0 {
					newVal = maxVal
				} else {
					newVal = minVal
				}
				bfSet(bm, offset, bits, signed, newVal)
				return newVal, true
			}
		} else {
			uold := uint64(old)
			uincr := uint64(incr)
			umax := uint64(maxVal)
			if incr > 0 && uold > umax-uincr {
				newVal = maxVal
				bfSet(bm, offset, bits, signed, newVal)
				return newVal, true
			}
			if incr < 0 && uold < uint64(-incr) {
				newVal = minVal
				bfSet(bm, offset, bits, signed, newVal)
				return newVal, true
			}
		}
		bfSet(bm, offset, bits, signed, newVal)
		return newVal, false
	}

	if overflow == "fail" {
		var minVal, maxVal int64
		if signed {
			minVal = -(1 << (bits - 1))
			maxVal = (1 << (bits - 1)) - 1
		} else {
			minVal = 0
			maxVal = (1 << bits) - 1
		}
		if signed {
			if (incr > 0 && old > maxVal-incr) || (incr < 0 && old < minVal-incr) {
				return 0, true
			}
		} else {
			uold := uint64(old)
			uincr := uint64(incr)
			umax := uint64(maxVal)
			if incr > 0 && uold > umax-uincr {
				return 0, true
			}
			if incr < 0 && uold < uint64(-incr) {
				return 0, true
			}
		}
		bfSet(bm, offset, bits, signed, newVal)
		return newVal, false
	}

	return 0, false
}

// bfParseSubcommand parses a type string and offset string, returning bits, signed, offset.
func bfParseSubcommand(typeStr, offsetStr string) (bits int, signed bool, offset int64, errReply protocol.ErrorReply) {
	bits, signed, err := parseBitfieldType(typeStr)
	if err != nil {
		return 0, false, 0, protocol.MakeErrReply("ERR invalid type specifier")
	}
	offset, err = parseBitfieldOffset(offsetStr, bits)
	if err != nil {
		return 0, false, 0, protocol.MakeErrReply("ERR invalid bit offset")
	}
	return bits, signed, offset, nil
}

// execBitfield implements the BITFIELD command.
func execBitfield(db *DB, args [][]byte) redis.Reply {
	if len(args) < 1 {
		return protocol.MakeErrReply("ERR wrong number of arguments for 'bitfield' command")
	}
	key := string(args[0])

	bm, isNew, errReply := db.getOrInitBitmap(key)
	if errReply != nil {
		return errReply
	}

	overflow := "wrap" // default
	results := make([]redis.Reply, 0)
	i := 1
	for i < len(args) {
		subcmd := strings.ToUpper(string(args[i]))
		i++

		switch subcmd {
		case "OVERFLOW":
			if i >= len(args) {
				return protocol.MakeErrReply("ERR syntax error")
			}
			overflow = strings.ToUpper(string(args[i]))
			i++
			if overflow != "WRAP" && overflow != "SAT" && overflow != "FAIL" {
				return protocol.MakeErrReply("ERR invalid OVERFLOW type specified")
			}
			overflow = strings.ToLower(overflow)

		case "GET":
			if i+2 > len(args) {
				return protocol.MakeErrReply("ERR syntax error")
			}
			bits, signed, offset, errReply := bfParseSubcommand(string(args[i]), string(args[i+1]))
			if errReply != nil {
				return errReply
			}
			i += 2
			val := bfGet(bm, offset, bits, signed)
			results = append(results, protocol.MakeIntReply(val))

		case "SET":
			if i+3 > len(args) {
				return protocol.MakeErrReply("ERR syntax error")
			}
			bits, signed, offset, errReply := bfParseSubcommand(string(args[i]), string(args[i+1]))
			if errReply != nil {
				return errReply
			}
			val, err := strconv.ParseInt(string(args[i+2]), 10, 64)
			if err != nil {
				return protocol.MakeErrReply("ERR invalid bitfield value")
			}
			i += 3
			old := bfSet(bm, offset, bits, signed, val)
			results = append(results, protocol.MakeIntReply(old))

		case "INCRBY":
			if i+3 > len(args) {
				return protocol.MakeErrReply("ERR syntax error")
			}
			bits, signed, offset, errReply := bfParseSubcommand(string(args[i]), string(args[i+1]))
			if errReply != nil {
				return errReply
			}
			incr, err := strconv.ParseInt(string(args[i+2]), 10, 64)
			if err != nil {
				return protocol.MakeErrReply("ERR invalid bitfield value")
			}
			i += 3
			newVal, overflowed := bfIncrby(bm, offset, bits, signed, incr, overflow)
			if overflowed && overflow == "fail" {
				results = append(results, &protocol.NullBulkReply{})
			} else {
				results = append(results, protocol.MakeIntReply(newVal))
			}

		default:
			return protocol.MakeErrReply("ERR syntax error")
		}
	}

	// Persist the bitmap
	if isNew {
		db.PutEntity(key, &database.DataEntity{Data: object.MakeRawStringObject(bm.ToBytes())})
	} else {
		entity, _ := db.GetEntity(key)
		if robj, ok := entity.Data.(*object.Robj); ok {
			robj.Ptr = bm.ToBytes()
			robj.Encoding = object.EncRaw
		}
	}
	db.addAof(utils.ToCmdLine3("bitfield", args...))

	return protocol.MakeMultiRawReply(results)
}

// execBitfieldRO implements the BITFIELD_RO command (read-only, GET only).
func execBitfieldRO(db *DB, args [][]byte) redis.Reply {
	if len(args) < 1 {
		return protocol.MakeErrReply("ERR wrong number of arguments for 'bitfield_ro' command")
	}
	key := string(args[0])

	bs, errReply := db.getAsString(key)
	if errReply != nil {
		return errReply
	}
	bm := bitmap.FromBytes(bs)

	results := make([]redis.Reply, 0)
	i := 1
	for i < len(args) {
		subcmd := strings.ToUpper(string(args[i]))
		i++

		switch subcmd {
		case "GET":
			if i+2 > len(args) {
				return protocol.MakeErrReply("ERR syntax error")
			}
			bits, signed, offset, errReply := bfParseSubcommand(string(args[i]), string(args[i+1]))
			if errReply != nil {
				return errReply
			}
			i += 2
			if bs == nil {
				results = append(results, &protocol.NullBulkReply{})
			} else {
				val := bfGet(bm, offset, bits, signed)
				results = append(results, protocol.MakeIntReply(val))
			}

		default:
			return protocol.MakeErrReply("ERR BITFIELD_RO only supports the GET subcommand")
		}
	}

	return protocol.MakeMultiRawReply(results)
}

func init() {
	registerCommand("Bitfield", execBitfield, writeFirstKey, nil, -2, flagWrite).
		attachCommandExtra([]string{redisFlagWrite, redisFlagDenyOOM}, 1, 1, 1).
		attachNotify(notifyString, "setbit")
	registerCommand("bitfield_ro", execBitfieldRO, readFirstKey, nil, -2, flagReadOnly).
		attachCommandExtra([]string{redisFlagReadonly}, 1, 1, 1)
}
