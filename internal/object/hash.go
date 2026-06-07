package object

import (
	"sort"

	"github.com/amemiya02/hayakv/internal/datastruct/dict"
	"github.com/amemiya02/hayakv/internal/lib/wildcard"
)

// NewHashFromDict creates a Hash backed by an existing dict.Dict (hashtable encoding).
func NewHashFromDict(d dict.Dict) *Hash {
	return &Hash{
		hashtable:  d,
		isListpack: false,
	}
}

// RandomDistinctKeys returns up to limit distinct random field names from the hash.
func (h *Hash) RandomDistinctKeys(limit int) []string {
	size := limit
	if size > h.Len() {
		size = h.Len()
	}
	result := make([]string, 0, size)
	h.ForEach(func(field string, value interface{}) bool {
		if len(result) >= size {
			return false
		}
		result = append(result, field)
		return true
	})
	return result
}

// RandomKeys returns up to limit random field names from the hash (may contain duplicates).
func (h *Hash) RandomKeys(limit int) []string {
	result := make([]string, 0, limit)
	// Collect all keys first, then pick randomly by cycling
	var allKeys []string
	h.ForEach(func(field string, value interface{}) bool {
		allKeys = append(allKeys, field)
		return true
	})
	if len(allKeys) == 0 {
		return result
	}
	for i := 0; i < limit; i++ {
		result = append(result, allKeys[i%len(allKeys)])
	}
	return result
}

// Keys returns all field names in the hash.
func (h *Hash) Keys() []string {
	result := make([]string, 0, h.Len())
	h.ForEach(func(field string, value interface{}) bool {
		result = append(result, field)
		return true
	})
	return result
}

// Scan performs a cursor-based scan over hash fields.
// Returns matching key-value pairs as [][]byte and the next cursor.
// This is a simplified implementation: collects all keys, sorts them,
// then returns a slice based on cursor position.
func (h *Hash) Scan(cursor int, count int, pattern string) ([][]byte, int) {
	// Collect all keys
	allKeys := h.Keys()
	sort.Strings(allKeys)

	total := len(allKeys)
	if total == 0 {
		return nil, 0
	}

	// Compile pattern
	matchKey, err := wildcard.CompilePattern(pattern)
	if err != nil {
		return nil, -1
	}

	// Build result from cursor position
	result := make([][]byte, 0)
	i := cursor
	for i < total && len(result) < count*2 {
		k := allKeys[i]
		if pattern == "*" || matchKey.IsMatch(k) {
			val, ok := h.Get(k)
			if ok {
				result = append(result, []byte(k))
				switch v := val.(type) {
				case []byte:
					result = append(result, v)
				case string:
					result = append(result, []byte(v))
				default:
					result = append(result, nil)
				}
			}
		}
		i++
	}

	// Calculate next cursor
	var nextCursor int
	if i >= total {
		nextCursor = 0
	} else {
		nextCursor = i
	}

	return result, nextCursor
}
