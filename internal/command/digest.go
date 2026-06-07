package database

import (
	"crypto/sha1"
	"fmt"
	"sort"
	"strconv"
	"time"

	"github.com/amemiya02/hayakv/internal/iface/database"
	"github.com/amemiya02/hayakv/internal/object"
)

// sha1Bytes returns the SHA1 hash of the given bytes.
func sha1Bytes(data []byte) [20]byte {
	return sha1.Sum(data)
}

// mixDigest XORs src into dst in place.
func mixDigest(dst *[20]byte, src [20]byte) {
	for i := range dst {
		dst[i] ^= src[i]
	}
}

// digestString returns the SHA1 digest of a string value.
func digestString(val []byte) [20]byte {
	return sha1Bytes(val)
}

// digestKey produces an encoding-independent digest for a single key and its
// value. For ordered types (list, zset) the element index is mixed in; for
// unordered types (set, hash) only element contents are mixed, making the
// result independent of insertion order.
func digestKey(key string, entity *database.DataEntity, expiration *time.Time) [20]byte {
	var d [20]byte

	// Mix in the key name.
	keyHash := sha1Bytes([]byte(key))
	mixDigest(&d, keyHash)

	// Mix in TTL if present.
	if expiration != nil {
		ttlMs := expiration.UnixMilli()
		ttlHash := sha1Bytes([]byte("__ttl__" + strconv.FormatInt(ttlMs, 10)))
		mixDigest(&d, ttlHash)
	}

	// Mix in the value based on its concrete type.
	// Unwrap Robj if present.
	var data interface{} = entity.Data
	if robj, ok := data.(*object.Robj); ok {
		data = robj.Ptr
	}

	switch v := data.(type) {
	case []byte:
		// Plain string value.
		mixDigest(&d, digestString(v))

	case *object.Hash:
		// Hash: field-value pairs, order-independent.
		// Collect pairs, sort by field, then hash each (field,value) pair.
		type fieldValuePair struct {
			field string
			value interface{}
		}
		var pairs []fieldValuePair
		v.ForEach(func(field string, value interface{}) bool {
			pairs = append(pairs, fieldValuePair{field, value})
			return true
		})
		sort.Slice(pairs, func(i, j int) bool {
			return pairs[i].field < pairs[j].field
		})
		for _, p := range pairs {
			// Hash field+0x00+value as a single blob to bind the association.
			// XORing field and value separately would lose pairing (e.g.
			// {a:1,b:2} and {a:2,b:1} would collide).
			blob := append(append([]byte(p.field), 0), valueToBytes(p.value)...)
			mixDigest(&d, sha1Bytes(blob))
		}

	case *object.Set:
		// Set: members, order-independent.
		type memberEntry struct {
			member string
		}
		var members []memberEntry
		v.ForEach(func(member string) bool {
			members = append(members, memberEntry{member})
			return true
		})
		sort.Slice(members, func(i, j int) bool {
			return members[i].member < members[j].member
		})
		for _, m := range members {
			memberHash := sha1Bytes([]byte(m.member))
			mixDigest(&d, memberHash)
		}

	case *object.ZSet:
		// Sorted set: member-score pairs, order-independent (sorted by member
		// for canonical form, but score is included in the hash).
		type memberScorePair struct {
			member string
			score  float64
		}
		var pairs []memberScorePair
		v.ForEach(func(member string, score float64) bool {
			pairs = append(pairs, memberScorePair{member, score})
			return true
		})
		sort.Slice(pairs, func(i, j int) bool {
			return pairs[i].member < pairs[j].member
		})
		for _, p := range pairs {
			// Hash member+0x00+score as a single blob to bind the association.
			scoreStr := strconv.FormatFloat(p.score, 'f', -1, 64)
			blob := append(append([]byte(p.member), 0), []byte(scoreStr)...)
			mixDigest(&d, sha1Bytes(blob))
		}

	case *object.List:
		// List: ordered, so index is mixed in.
		v.ForEach(func(i int, val interface{}) bool {
			idxHash := sha1Bytes([]byte(strconv.Itoa(i)))
			valBytes := valueToBytes(val)
			valHash := sha1Bytes(valBytes)
			mixDigest(&d, idxHash)
			mixDigest(&d, valHash)
			return true
		})

	default:
		// Unknown type — hash the fmt representation as a fallback.
		fallbackHash := sha1Bytes([]byte(fmt.Sprintf("%v", v)))
		mixDigest(&d, fallbackHash)
	}

	return d
}

// valueToBytes converts a value to []byte for hashing.
func valueToBytes(val interface{}) []byte {
	switch v := val.(type) {
	case []byte:
		return v
	case string:
		return []byte(v)
	case int64:
		return []byte(strconv.FormatInt(v, 10))
	case float64:
		return []byte(strconv.FormatFloat(v, 'f', -1, 64))
	default:
		return []byte(fmt.Sprintf("%v", v))
	}
}

// datasetDigest computes the XOR of all key digests across all databases.
// This is encoding-independent: the same logical dataset always produces
// the same digest regardless of internal encoding (listpack vs hashtable,
// intset vs listpack, etc.).
func datasetDigest(server *Server) [20]byte {
	var d [20]byte

	dbCount := len(server.dbSet)
	for i := 0; i < dbCount; i++ {
		server.ForEach(i, func(key string, data *database.DataEntity, expiration *time.Time) bool {
			kd := digestKey(key, data, expiration)
			mixDigest(&d, kd)
			return true
		})
	}

	return d
}
