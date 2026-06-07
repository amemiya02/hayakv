package object

import (
	"sort"
	"strconv"

	"github.com/amemiya02/hayakv/internal/datastruct/dict"
	"github.com/amemiya02/hayakv/internal/datastruct/list"
	"github.com/amemiya02/hayakv/internal/datastruct/sortedset"
	"github.com/amemiya02/hayakv/internal/lib/wildcard"
)

// Alias types for convenience
type SortedSet = sortedset.SortedSet
type Element = sortedset.Element
type QuickList = list.QuickList

// NewQuickList creates a new QuickList
func NewQuickList() *QuickList {
	return list.NewQuickList()
}

// formatFloat formats a float64 as string
func formatFloat(f float64) string {
	return strconv.FormatFloat(f, 'f', -1, 64)
}

// parseFloat parses a string as float64
func parseFloat(s string) (float64, error) {
	return strconv.ParseFloat(s, 64)
}

// Hash represents a hash object that can be encoded as listpack or hashtable
type Hash struct {
	listpack   *Listpack
	hashtable  dict.Dict
	isListpack bool
}

// NewHash creates a new hash object
func NewHash() *Hash {
	return &Hash{
		listpack:   NewListpack(),
		isListpack: true,
	}
}

// Get returns the value for the given field
func (h *Hash) Get(field string) (interface{}, bool) {
	if h.isListpack {
		// Search in listpack (field, value, field, value, ...)
		for i := 0; i < h.listpack.Len(); i += 2 {
			val, ok := h.listpack.Get(i)
			if !ok {
				break
			}
			if str, ok := val.(string); ok && str == field {
				// Found field, get value at i+1
				value, ok := h.listpack.Get(i + 1)
				if !ok {
					return nil, false
				}
				return value, true
			}
		}
		return nil, false
	}
	return h.hashtable.Get(field)
}

// Put sets a field-value pair
func (h *Hash) Put(field string, value interface{}) int {
	if h.isListpack {
		// Check if field exists
		found := false
		h.listpack.Range(func(i int, val interface{}) bool {
			if i%2 == 0 {
				if str, ok := val.(string); ok && str == field {
					found = true
					return false
				}
			}
			return true
		})

		if found {
			// Update existing field - need to rebuild listpack
			h.rebuildListpackWithUpdate(field, value)
			return 0
		}

		// Add new field-value pair
		h.listpack.AppendStr(field)
		switch v := value.(type) {
		case []byte:
			h.listpack.AppendStr(string(v))
		case string:
			h.listpack.AppendStr(v)
		default:
			h.listpack.AppendStr("")
		}

		// Check if we should convert to hashtable
		h.maybeConvertToHashtable()
		return 1
	}

	return h.hashtable.Put(field, value)
}

// PutIfAbsent sets a field-value pair only if field doesn't exist
func (h *Hash) PutIfAbsent(field string, value interface{}) int {
	if h.isListpack {
		// Check if field exists
		found := false
		h.listpack.Range(func(i int, val interface{}) bool {
			if i%2 == 0 {
				if str, ok := val.(string); ok && str == field {
					found = true
					return false
				}
			}
			return true
		})

		if found {
			return 0
		}

		// Add new field-value pair
		h.listpack.AppendStr(field)
		switch v := value.(type) {
		case []byte:
			h.listpack.AppendStr(string(v))
		case string:
			h.listpack.AppendStr(v)
		default:
			h.listpack.AppendStr("")
		}

		// Check if we should convert to hashtable
		h.maybeConvertToHashtable()
		return 1
	}

	return h.hashtable.PutIfAbsent(field, value)
}

// Remove removes a field from the hash
func (h *Hash) Remove(field string) (interface{}, int) {
	if h.isListpack {
		// Find and remove field-value pair
		newLp := NewListpack()
		removed := false
		for i := 0; i < h.listpack.Len(); i += 2 {
			val, ok := h.listpack.Get(i)
			if !ok {
				break
			}
			str, ok := val.(string)
			if !ok {
				continue
			}
			if str == field {
				removed = true
				continue
			}
			// Keep this field-value pair
			newLp.AppendStr(str)
			value, ok := h.listpack.Get(i + 1)
			if ok {
				switch v := value.(type) {
				case string:
					newLp.AppendStr(v)
				case int64:
					newLp.AppendInt(v)
				}
			}
		}

		if !removed {
			return nil, 0
		}

		h.listpack = newLp
		return field, 1
	}

	return h.hashtable.Remove(field)
}

// Len returns the number of fields in the hash
func (h *Hash) Len() int {
	if h.isListpack {
		return h.listpack.Len() / 2
	}
	return h.hashtable.Len()
}

// ForEach iterates over all field-value pairs
func (h *Hash) ForEach(fn func(field string, value interface{}) bool) {
	if h.isListpack {
		var field string
		h.listpack.Range(func(i int, val interface{}) bool {
			if i%2 == 0 {
				// Field
				if str, ok := val.(string); ok {
					field = str
				}
			} else {
				// Value
				if !fn(field, val) {
					return false
				}
			}
			return true
		})
		return
	}

	h.hashtable.ForEach(func(key string, val interface{}) bool {
		return fn(key, val)
	})
}

// IsListpack returns true if the hash is using listpack encoding
func (h *Hash) IsListpack() bool {
	return h.isListpack
}

// CurrentEncoding returns the current encoding of the hash object.
func (h *Hash) CurrentEncoding() Encoding {
	if h.isListpack {
		return EncListpack
	}
	return EncHashtable
}

// rebuildListpackWithUpdate rebuilds the listpack with an updated field
func (h *Hash) rebuildListpackWithUpdate(field string, value interface{}) {
	newLp := NewListpack()
	h.listpack.Range(func(i int, val interface{}) bool {
		if i%2 == 0 {
			// Field
			if str, ok := val.(string); ok {
				newLp.AppendStr(str)
			}
		} else {
			// Value
			if str, ok := val.(string); ok {
				// Check if this is the field we're updating
				if i > 0 {
					prevVal, _ := h.listpack.Get(i - 1)
					if prevStr, ok := prevVal.(string); ok && prevStr == field {
						// Update value
						switch v := value.(type) {
						case []byte:
							newLp.AppendStr(string(v))
						case string:
							newLp.AppendStr(v)
						default:
							newLp.AppendStr("")
						}
						return true
					}
				}
				newLp.AppendStr(str)
			}
		}
		return true
	})
	h.listpack = newLp
}

// maybeConvertToHashtable checks if we should convert from listpack to hashtable
func (h *Hash) maybeConvertToHashtable() {
	// TODO: Check config thresholds
	// For now, convert if we have more than 128 fields
	if h.listpack.Len()/2 > 128 {
		h.convertToHashtable()
	}
}

// convertToHashtable converts from listpack to hashtable encoding
func (h *Hash) convertToHashtable() {
	newDict := dict.MakeSimple()
	var field string
	h.listpack.Range(func(i int, val interface{}) bool {
		if i%2 == 0 {
			// Field
			if str, ok := val.(string); ok {
				field = str
			}
		} else {
			// Value
			switch v := val.(type) {
			case string:
				newDict.Put(field, []byte(v))
			case []byte:
				newDict.Put(field, v)
			}
		}
		return true
	})

	h.hashtable = newDict
	h.isListpack = false
	h.listpack = nil
}

// GetAsDict returns the hash as a Dict (for backward compatibility)
func (h *Hash) GetAsDict() dict.Dict {
	if h.isListpack {
		// Convert to hashtable first
		h.convertToHashtable()
	}
	return h.hashtable
}

// Set represents a set object that can be encoded as intset, listpack, or hashtable
type Set struct {
	intset    *Intset
	listpack  *Listpack
	hashtable dict.Dict
	encoding  string // "intset", "listpack", "hashtable"
}

// NewSet creates a new set object
func NewSet() *Set {
	return &Set{
		intset:   NewIntset(),
		encoding: "intset",
	}
}

// Add adds a member to the set
func (s *Set) Add(member string) int {
	if s == nil {
		return 0
	}
	if s.encoding == "intset" {
		// Try to parse as int64
		if v, ok := isInt(member); ok {
			if s.intset.Add(v) {
				// Check if we should convert to listpack
				if s.intset.Len() > 512 {
					s.convertToListpack()
					// After conversion, check if we should convert to hashtable
					if s.listpack.Len() > 128 {
						s.convertToHashtable()
					}
				}
				return 1
			}
			return 0
		}
		// Not an int, convert to listpack
		s.convertToListpack()
		return s.Add(member)
	}

	if s.encoding == "listpack" {
		// Check if member exists
		found := false
		s.listpack.Range(func(i int, val interface{}) bool {
			if str, ok := val.(string); ok && str == member {
				found = true
				return false
			}
			return true
		})
		if found {
			return 0
		}

		s.listpack.AppendStr(member)
		// Check if we should convert to hashtable
		// Redis default: set-max-listpack-entries 128
		if s.listpack.Len() > 128 {
			s.convertToHashtable()
		}
		return 1
	}

	// Hashtable encoding
	return s.hashtable.Put(member, nil)
}

// Has checks if the set contains the given member
func (s *Set) Has(member string) bool {
	if s == nil {
		return false
	}
	if s.encoding == "intset" {
		if v, ok := isInt(member); ok {
			return s.intset.Contains(v)
		}
		return false
	}

	if s.encoding == "listpack" {
		found := false
		s.listpack.Range(func(i int, val interface{}) bool {
			if str, ok := val.(string); ok && str == member {
				found = true
				return false
			}
			return true
		})
		return found
	}

	_, exists := s.hashtable.Get(member)
	return exists
}

// Remove removes a member from the set
func (s *Set) Remove(member string) int {
	if s == nil {
		return 0
	}
	if s.encoding == "intset" {
		if v, ok := isInt(member); ok {
			if s.intset.Remove(v) {
				return 1
			}
		}
		return 0
	}

	if s.encoding == "listpack" {
		// Find and remove member
		newLp := NewListpack()
		removed := false
		s.listpack.Range(func(i int, val interface{}) bool {
			if str, ok := val.(string); ok && str == member {
				removed = true
				return true
			}
			if str, ok := val.(string); ok {
				newLp.AppendStr(str)
			}
			return true
		})
		if !removed {
			return 0
		}
		s.listpack = newLp
		return 1
	}

	_, ret := s.hashtable.Remove(member)
	return ret
}

// Len returns the number of members in the set
func (s *Set) Len() int {
	if s == nil {
		return 0
	}
	if s.encoding == "intset" {
		return s.intset.Len()
	}
	if s.encoding == "listpack" {
		return s.listpack.Len()
	}
	return s.hashtable.Len()
}

// ForEach iterates over all members in the set
func (s *Set) ForEach(fn func(member string) bool) {
	if s == nil {
		return
	}
	if s.encoding == "intset" {
		s.intset.Range(func(v int64) bool {
			return fn(formatInt64(v))
		})
		return
	}

	if s.encoding == "listpack" {
		s.listpack.Range(func(i int, val interface{}) bool {
			if str, ok := val.(string); ok {
				return fn(str)
			}
			return true
		})
		return
	}

	s.hashtable.ForEach(func(key string, val interface{}) bool {
		return fn(key)
	})
}

// Encoding returns the current encoding type
func (s *Set) Encoding() string {
	return s.encoding
}

// CurrentEncoding returns the current encoding as an Encoding enum.
func (s *Set) CurrentEncoding() Encoding {
	switch s.encoding {
	case "intset":
		return EncIntset
	case "listpack":
		return EncListpack
	default:
		return EncHashtable
	}
}

// convertToListpack converts from intset to listpack encoding
func (s *Set) convertToListpack() {
	newLp := NewListpack()
	s.intset.Range(func(v int64) bool {
		newLp.AppendStr(formatInt64(v))
		return true
	})
	s.listpack = newLp
	s.encoding = "listpack"
	s.intset = nil
}

// convertToHashtable converts from listpack to hashtable encoding
func (s *Set) convertToHashtable() {
	newDict := dict.MakeSimple()
	s.listpack.Range(func(i int, val interface{}) bool {
		if str, ok := val.(string); ok {
			newDict.Put(str, nil)
		}
		return true
	})
	s.hashtable = newDict
	s.encoding = "hashtable"
	s.listpack = nil
}

// GetAsSet returns the set as a *set.Set (for backward compatibility)
func (s *Set) GetAsSet() *Set {
	// This is a placeholder - in practice, you'd convert to the actual set.Set type
	return s
}

// RandomDistinctMembers randomly returns up to limit distinct members.
func (s *Set) RandomDistinctMembers(limit int) []string {
	members := s.ToSlice()
	if limit > len(members) {
		limit = len(members)
	}
	result := make([]string, limit)
	// Simple Fisher-Yates partial shuffle
	for i := 0; i < limit; i++ {
		j := i + int(simpleRand()%uint32(len(members)-i))
		members[i], members[j] = members[j], members[i]
		result[i] = members[i]
	}
	return result
}

// RandomMembers randomly returns up to limit members (may contain duplicates).
func (s *Set) RandomMembers(limit int) []string {
	members := s.ToSlice()
	if len(members) == 0 {
		return nil
	}
	result := make([]string, limit)
	for i := 0; i < limit; i++ {
		result[i] = members[simpleRand()%uint32(len(members))]
	}
	return result
}

// ToSlice returns all members as a string slice.
func (s *Set) ToSlice() []string {
	slice := make([]string, 0, s.Len())
	s.ForEach(func(member string) bool {
		slice = append(slice, member)
		return true
	})
	return slice
}

// SetScan scans members matching the pattern.
func (s *Set) SetScan(cursor int, count int, pattern string) ([][]byte, int) {
	all := s.ToSlice()
	// Sort for deterministic scan order
	sort.Strings(all)
	total := len(all)
	if total == 0 {
		return nil, 0
	}
	matchKey, err := wildcard.CompilePattern(pattern)
	if err != nil {
		return nil, -1
	}
	result := make([][]byte, 0)
	i := cursor
	for i < total && len(result) < count {
		m := all[i]
		if pattern == "*" || matchKey.IsMatch(m) {
			result = append(result, []byte(m))
		}
		i++
	}
	var nextCursor int
	if i >= total {
		nextCursor = 0
	} else {
		nextCursor = i
	}
	return result, nextCursor
}

// simpleRand provides a simple pseudo-random number generator.
// Not cryptographically secure, but sufficient for random member selection.
var simpleRandSeed uint32 = 12345

func simpleRand() uint32 {
	simpleRandSeed = simpleRandSeed*1103515245 + 12345
	return simpleRandSeed
}

// ZSet represents a sorted set object that can be encoded as listpack or skiplist
type ZSet struct {
	listpack   *Listpack
	skiplist   *SortedSet
	isListpack bool
}

// NewZSet creates a new sorted set object
func NewZSet() *ZSet {
	return &ZSet{
		listpack:   NewListpack(),
		isListpack: true,
	}
}

// Add adds a member with score to the sorted set
func (z *ZSet) Add(member string, score float64) bool {
	if z.isListpack {
		// Check if member exists
		found := false
		z.listpack.Range(func(i int, val interface{}) bool {
			if i%2 == 0 {
				if str, ok := val.(string); ok && str == member {
					found = true
					return false
				}
			}
			return true
		})

		if found {
			// Update score - rebuild listpack
			z.rebuildListpackWithScore(member, score)
			return false
		}

		// Add new member-score pair
		z.listpack.AppendStr(member)
		z.listpack.AppendStr(formatFloat(score))

		// Check if we should convert to skiplist
		if z.listpack.Len()/2 > 128 {
			z.convertToSkiplist()
		}
		return true
	}

	return z.skiplist.Add(member, score)
}

// Get returns the score for the given member
func (z *ZSet) Get(member string) (float64, bool) {
	if z.isListpack {
		for i := 0; i < z.listpack.Len(); i += 2 {
			val, ok := z.listpack.Get(i)
			if !ok {
				break
			}
			if str, ok := val.(string); ok && str == member {
				// Get score at i+1
				scoreVal, ok := z.listpack.Get(i + 1)
				if !ok {
					return 0, false
				}
				if scoreStr, ok := scoreVal.(string); ok {
					score, err := parseFloat(scoreStr)
					if err != nil {
						return 0, false
					}
					return score, true
				}
			}
		}
		return 0, false
	}

	elem, ok := z.skiplist.Get(member)
	if !ok {
		return 0, false
	}
	return elem.Score, true
}

// Remove removes a member from the sorted set
func (z *ZSet) Remove(member string) bool {
	if z.isListpack {
		// Find and remove member-score pair
		newLp := NewListpack()
		removed := false
		for i := 0; i < z.listpack.Len(); i += 2 {
			val, ok := z.listpack.Get(i)
			if !ok {
				break
			}
			str, ok := val.(string)
			if !ok {
				continue
			}
			if str == member {
				removed = true
				continue
			}
			// Keep this member-score pair
			newLp.AppendStr(str)
			scoreVal, ok := z.listpack.Get(i + 1)
			if ok {
				if scoreStr, ok := scoreVal.(string); ok {
					newLp.AppendStr(scoreStr)
				}
			}
		}
		if !removed {
			return false
		}
		z.listpack = newLp
		return true
	}

	return z.skiplist.Remove(member)
}

// Len returns the number of members in the sorted set
func (z *ZSet) Len() int {
	if z.isListpack {
		return z.listpack.Len() / 2
	}
	return int(z.skiplist.Len())
}

// ForEach iterates over all members in the sorted set
func (z *ZSet) ForEach(fn func(member string, score float64) bool) {
	if z.isListpack {
		var member string
		z.listpack.Range(func(i int, val interface{}) bool {
			if i%2 == 0 {
				// Member
				if str, ok := val.(string); ok {
					member = str
				}
			} else {
				// Score
				if str, ok := val.(string); ok {
					score, err := parseFloat(str)
					if err == nil {
						if !fn(member, score) {
							return false
						}
					}
				}
			}
			return true
		})
		return
	}

	// For skiplist, iterate in order
	z.skiplist.ForEachByRank(0, z.skiplist.Len(), false, func(elem *Element) bool {
		return fn(elem.Member, elem.Score)
	})
}

// IsListpack returns true if the sorted set is using listpack encoding
func (z *ZSet) IsListpack() bool {
	return z.isListpack
}

// CurrentEncoding returns the current encoding of the zset object.
func (z *ZSet) CurrentEncoding() Encoding {
	if z.isListpack {
		return EncListpack
	}
	return EncSkiplist
}

// rebuildListpackWithScore rebuilds the listpack with an updated score
func (z *ZSet) rebuildListpackWithScore(member string, score float64) {
	newLp := NewListpack()
	z.listpack.Range(func(i int, val interface{}) bool {
		if i%2 == 0 {
			// Member
			if str, ok := val.(string); ok {
				newLp.AppendStr(str)
			}
		} else {
			// Score
			if str, ok := val.(string); ok {
				// Check if this is the member we're updating
				if i > 0 {
					prevVal, _ := z.listpack.Get(i - 1)
					if prevStr, ok := prevVal.(string); ok && prevStr == member {
						// Update score
						newLp.AppendStr(formatFloat(score))
						return true
					}
				}
				newLp.AppendStr(str)
			}
		}
		return true
	})
	z.listpack = newLp
}

// convertToSkiplist converts from listpack to skiplist encoding
func (z *ZSet) convertToSkiplist() {
	newZSet := sortedset.Make()
	z.listpack.Range(func(i int, val interface{}) bool {
		if i%2 == 0 {
			// Member
			if member, ok := val.(string); ok {
				// Get score at i+1
				scoreVal, ok := z.listpack.Get(i + 1)
				if ok {
					if scoreStr, ok := scoreVal.(string); ok {
						score, err := parseFloat(scoreStr)
						if err == nil {
							newZSet.Add(member, score)
						}
					}
				}
			}
		}
		return true
	})
	z.skiplist = newZSet
	z.isListpack = false
	z.listpack = nil
}

// GetAsSortedSet returns the zset as a *SortedSet (for backward compatibility)
func (z *ZSet) GetAsSortedSet() *SortedSet {
	if z.isListpack {
		z.convertToSkiplist()
	}
	return z.skiplist
}

// zsetEntry is a sorted (member, score) pair used by collectSorted.
type zsetEntry struct {
	Member string
	Score  float64
}

// collectSorted reads all member-score pairs from the listpack, sorts them
// by score (then by member for equal scores), and returns the slice.
// This avoids converting to skiplist for small zsets on read operations.
func (z *ZSet) collectSorted() []zsetEntry {
	entries := make([]zsetEntry, 0, z.listpack.Len()/2)
	var member string
	z.listpack.Range(func(i int, val interface{}) bool {
		if i%2 == 0 {
			if str, ok := val.(string); ok {
				member = str
			}
		} else {
			if str, ok := val.(string); ok {
				score, err := parseFloat(str)
				if err == nil {
					entries = append(entries, zsetEntry{Member: member, Score: score})
				}
			}
		}
		return true
	})
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Score != entries[j].Score {
			return entries[i].Score < entries[j].Score
		}
		return entries[i].Member < entries[j].Member
	})
	return entries
}

// GetRank returns the rank of the given member
// When desc=true, rank 0 is the highest-score element (matching skiplist semantics).
func (z *ZSet) GetRank(member string, desc bool) int64 {
	if z.isListpack {
		entries := z.collectSorted()
		if desc {
			// desc: rank 0 = highest score (last in sorted entries)
			for i := len(entries) - 1; i >= 0; i-- {
				if entries[i].Member == member {
					return int64(len(entries) - 1 - i)
				}
			}
		} else {
			for i, e := range entries {
				if e.Member == member {
					return int64(i)
				}
			}
		}
		return -1
	}
	return z.skiplist.GetRank(member, desc)
}

// RangeByRank returns members which rank within [start, stop)
// When desc=true, rank 0 is the highest-score element (matching skiplist semantics).
func (z *ZSet) RangeByRank(start, stop int64, desc bool) []*Element {
	if z.isListpack {
		entries := z.collectSorted()
		n := int64(len(entries))
		if start < 0 || start >= n {
			return nil
		}
		if stop > n {
			stop = n
		}
		if stop <= start {
			return nil
		}
		result := make([]*Element, 0, stop-start)
		if desc {
			// desc mode: rank 0 = highest score (entries[n-1]), rank k = entries[n-1-k]
			for i := start; i < stop; i++ {
				idx := n - 1 - i
				result = append(result, &Element{Member: entries[idx].Member, Score: entries[idx].Score})
			}
		} else {
			for i := start; i < stop; i++ {
				result = append(result, &Element{Member: entries[i].Member, Score: entries[i].Score})
			}
		}
		return result
	}
	return z.skiplist.RangeByRank(start, stop, desc)
}

// RangeCount returns the number of members within the given score/lex border range
func (z *ZSet) RangeCount(min, max sortedset.Border) int64 {
	if z.isListpack {
		entries := z.collectSorted()
		count := int64(0)
		for _, e := range entries {
			elem := &Element{Member: e.Member, Score: e.Score}
			if min.Less(elem) && max.Greater(elem) {
				count++
			}
		}
		return count
	}
	return z.skiplist.RangeCount(min, max)
}

// Range returns members within the given score/lex border range
// When desc=true, iteration starts from the highest-score element and offset
// skips from that end, matching skiplist semantics.
func (z *ZSet) Range(min, max sortedset.Border, offset, limit int64, desc bool) []*Element {
	if z.isListpack {
		entries := z.collectSorted()
		var filtered []zsetEntry
		for _, e := range entries {
			elem := &Element{Member: e.Member, Score: e.Score}
			if min.Less(elem) && max.Greater(elem) {
				filtered = append(filtered, e)
			}
		}
		if offset < 0 {
			return nil
		}
		n := int64(len(filtered))
		if n == 0 {
			return nil
		}
		result := make([]*Element, 0)
		if desc {
			// Start from highest score (last in filtered), skip offset, take limit
			i := n - 1 - offset
			for taken := int64(0); i >= 0 && (limit < 0 || taken < limit); taken++ {
				if !min.Less(&Element{Member: filtered[i].Member, Score: filtered[i].Score}) ||
					!max.Greater(&Element{Member: filtered[i].Member, Score: filtered[i].Score}) {
					break
				}
				result = append(result, &Element{Member: filtered[i].Member, Score: filtered[i].Score})
				i--
			}
		} else {
			// Start from lowest score (first in filtered), skip offset, take limit
			i := offset
			for taken := int64(0); i < n && (limit < 0 || taken < limit); taken++ {
				if !min.Less(&Element{Member: filtered[i].Member, Score: filtered[i].Score}) ||
					!max.Greater(&Element{Member: filtered[i].Member, Score: filtered[i].Score}) {
					break
				}
				result = append(result, &Element{Member: filtered[i].Member, Score: filtered[i].Score})
				i++
			}
		}
		return result
	}
	return z.skiplist.Range(min, max, offset, limit, desc)
}

// RemoveRange removes members within the given score/lex border range
func (z *ZSet) RemoveRange(min, max sortedset.Border) int64 {
	if z.isListpack {
		entries := z.collectSorted()
		var toRemove []string
		for _, e := range entries {
			elem := &Element{Member: e.Member, Score: e.Score}
			if min.Less(elem) && max.Greater(elem) {
				toRemove = append(toRemove, e.Member)
			}
		}
		removeSet := make(map[string]struct{}, len(toRemove))
		for _, m := range toRemove {
			removeSet[m] = struct{}{}
		}
		newLp := NewListpack()
		z.listpack.Range(func(i int, val interface{}) bool {
			if i%2 == 0 {
				if str, ok := val.(string); ok {
					if _, skip := removeSet[str]; skip {
						// Skip the score entry too by not appending member
						// We need a flag — use a sentinel approach
						return true
					}
					newLp.AppendStr(str)
				}
			} else {
				// Score — only append if the previous member wasn't removed
				// We need to check the previous member; simpler: rebuild entirely
				if str, ok := val.(string); ok {
					newLp.AppendStr(str)
				}
			}
			return true
		})
		// The above is wrong — we skip member but not score. Rebuild properly:
		newLp = NewListpack()
		for i := 0; i < z.listpack.Len(); i += 2 {
			memberVal, ok := z.listpack.Get(i)
			if !ok {
				break
			}
			member, ok := memberVal.(string)
			if !ok {
				continue
			}
			if _, skip := removeSet[member]; skip {
				continue
			}
			scoreVal, ok := z.listpack.Get(i + 1)
			if ok {
				if scoreStr, ok := scoreVal.(string); ok {
					newLp.AppendStr(member)
					newLp.AppendStr(scoreStr)
				}
			}
		}
		z.listpack = newLp
		return int64(len(toRemove))
	}
	return z.skiplist.RemoveRange(min, max)
}

// RemoveByRank removes members ranking within [start, stop)
func (z *ZSet) RemoveByRank(start, stop int64) int64 {
	if z.isListpack {
		entries := z.collectSorted()
		n := int64(len(entries))
		if start < 0 {
			start = 0
		}
		if stop > n {
			stop = n
		}
		if start >= stop {
			return 0
		}
		removeSet := make(map[string]struct{}, stop-start)
		for i := start; i < stop; i++ {
			removeSet[entries[i].Member] = struct{}{}
		}
		newLp := NewListpack()
		for i := 0; i < z.listpack.Len(); i += 2 {
			memberVal, ok := z.listpack.Get(i)
			if !ok {
				break
			}
			member, ok := memberVal.(string)
			if !ok {
				continue
			}
			if _, skip := removeSet[member]; skip {
				continue
			}
			scoreVal, ok := z.listpack.Get(i + 1)
			if ok {
				if scoreStr, ok := scoreVal.(string); ok {
					newLp.AppendStr(member)
					newLp.AppendStr(scoreStr)
				}
			}
		}
		z.listpack = newLp
		return int64(len(removeSet))
	}
	return z.skiplist.RemoveByRank(start, stop)
}

// PopMin removes and returns the count lowest-scoring members
func (z *ZSet) PopMin(count int) []*Element {
	if z.isListpack {
		entries := z.collectSorted()
		if count > len(entries) {
			count = len(entries)
		}
		removeSet := make(map[string]struct{}, count)
		result := make([]*Element, 0, count)
		for i := 0; i < count; i++ {
			removeSet[entries[i].Member] = struct{}{}
			result = append(result, &Element{Member: entries[i].Member, Score: entries[i].Score})
		}
		newLp := NewListpack()
		for i := 0; i < z.listpack.Len(); i += 2 {
			memberVal, ok := z.listpack.Get(i)
			if !ok {
				break
			}
			member, ok := memberVal.(string)
			if !ok {
				continue
			}
			if _, skip := removeSet[member]; skip {
				continue
			}
			scoreVal, ok := z.listpack.Get(i + 1)
			if ok {
				if scoreStr, ok := scoreVal.(string); ok {
					newLp.AppendStr(member)
					newLp.AppendStr(scoreStr)
				}
			}
		}
		z.listpack = newLp
		return result
	}
	return z.skiplist.PopMin(count)
}

// ForEachByRank visits members by rank
// When desc=true, rank 0 is the highest-score element (matching skiplist semantics).
func (z *ZSet) ForEachByRank(start, stop int64, desc bool, fn func(*Element) bool) {
	if z.isListpack {
		entries := z.collectSorted()
		n := int64(len(entries))
		if start < 0 || start >= n {
			return
		}
		if stop > n {
			stop = n
		}
		if stop <= start {
			return
		}
		if desc {
			// desc mode: rank 0 = highest score (entries[n-1]), rank k = entries[n-1-k]
			for i := start; i < stop; i++ {
				idx := n - 1 - i
				if !fn(&Element{Member: entries[idx].Member, Score: entries[idx].Score}) {
					return
				}
			}
		} else {
			for i := start; i < stop; i++ {
				if !fn(&Element{Member: entries[i].Member, Score: entries[i].Score}) {
					return
				}
			}
		}
		return
	}
	z.skiplist.ForEachByRank(start, stop, desc, fn)
}

// ZSetScan scans members matching the pattern
func (z *ZSet) ZSetScan(cursor, count int, pattern string) ([][]byte, int) {
	if z.isListpack {
		matchKey, err := wildcard.CompilePattern(pattern)
		if err != nil {
			return nil, -1
		}
		result := make([][]byte, 0)
		z.listpack.Range(func(i int, val interface{}) bool {
			if i%2 == 0 {
				if member, ok := val.(string); ok {
					if pattern == "*" || matchKey.IsMatch(member) {
						result = append(result, []byte(member))
						// Get score at i+1
						scoreVal, ok := z.listpack.Get(i + 1)
						if ok {
							if scoreStr, ok := scoreVal.(string); ok {
								result = append(result, []byte(scoreStr))
							}
						}
					}
				}
			}
			return true
		})
		return result, 0
	}
	return z.skiplist.ZSetScan(cursor, count, pattern)
}

// List represents a list object that can be encoded as listpack or quicklist
type List struct {
	listpack   *Listpack
	quicklist  *QuickList
	isListpack bool
}

// NewList creates a new list object
func NewList() *List {
	return &List{
		listpack:   NewListpack(),
		isListpack: true,
	}
}

// Add adds a value to the end of the list
func (l *List) Add(val interface{}) {
	if l.isListpack {
		switch v := val.(type) {
		case []byte:
			l.listpack.AppendStr(string(v))
		case string:
			l.listpack.AppendStr(v)
		default:
			l.listpack.AppendStr("")
		}

		// Check if we should convert to quicklist
		if l.listpack.Len() > 128 {
			l.convertToQuicklist()
		}
		return
	}

	l.quicklist.Add(val)
}

// listpackToBytes converts a listpack value to []byte for consistency with quicklist
func listpackToBytes(val interface{}) interface{} {
	switch v := val.(type) {
	case string:
		return []byte(v)
	case int64:
		return []byte(formatInt64(v))
	default:
		return val
	}
}

// Get returns the value at the given index
func (l *List) Get(index int) (interface{}, bool) {
	if l.isListpack {
		val, ok := l.listpack.Get(index)
		if !ok {
			return nil, false
		}
		return listpackToBytes(val), true
	}

	if index < 0 || index >= l.quicklist.Len() {
		return nil, false
	}
	return l.quicklist.Get(index), true
}

// Len returns the number of elements in the list
func (l *List) Len() int {
	if l.isListpack {
		return l.listpack.Len()
	}
	return l.quicklist.Len()
}

// ForEach iterates over all elements in the list
func (l *List) ForEach(fn func(i int, val interface{}) bool) {
	if l.isListpack {
		l.listpack.Range(func(i int, val interface{}) bool {
			return fn(i, listpackToBytes(val))
		})
		return
	}

	l.quicklist.ForEach(func(i int, val interface{}) bool {
		return fn(i, val)
	})
}

// IsListpack returns true if the list is using listpack encoding
func (l *List) IsListpack() bool {
	return l.isListpack
}

// CurrentEncoding returns the current encoding of the list object.
func (l *List) CurrentEncoding() Encoding {
	if l.isListpack {
		return EncListpack
	}
	return EncQuicklist
}

// convertToQuicklist converts from listpack to quicklist encoding
func (l *List) convertToQuicklist() {
	newQL := NewQuickList()
	l.listpack.Range(func(i int, val interface{}) bool {
		switch v := val.(type) {
		case string:
			newQL.Add([]byte(v))
		case int64:
			newQL.Add([]byte(formatInt64(v)))
		}
		return true
	})
	l.quicklist = newQL
	l.isListpack = false
	l.listpack = nil
}

// Insert inserts a value at the given index (existing elements shift right)
func (l *List) Insert(index int, val interface{}) {
	if l.isListpack {
		if index == l.listpack.Len() {
			l.Add(val)
			return
		}
		// Rebuild listpack with element inserted at index
		newLp := NewListpack()
		for i := 0; i < l.listpack.Len(); i++ {
			if i == index {
				l.appendVal(newLp, val)
			}
			elem, ok := l.listpack.Get(i)
			if ok {
				l.appendVal(newLp, elem)
			}
		}
		l.listpack = newLp
		// Check if we should convert to quicklist
		if l.listpack.Len() > 128 {
			l.convertToQuicklist()
		}
		return
	}

	l.quicklist.Insert(index, val)
}

// Remove removes the element at the given index and returns its value
func (l *List) Remove(index int) interface{} {
	if l.isListpack {
		if index < 0 || index >= l.listpack.Len() {
			return nil
		}
		var removed interface{}
		newLp := NewListpack()
		for i := 0; i < l.listpack.Len(); i++ {
			elem, ok := l.listpack.Get(i)
			if !ok {
				break
			}
			if i == index {
				removed = listpackToBytes(elem)
				continue
			}
			l.appendVal(newLp, elem)
		}
		l.listpack = newLp
		return removed
	}

	return l.quicklist.Remove(index)
}

// RemoveLast removes the last element and returns its value
func (l *List) RemoveLast() interface{} {
	if l.isListpack {
		if l.listpack.Len() == 0 {
			return nil
		}
		lastIdx := l.listpack.Len() - 1
		var removed interface{}
		newLp := NewListpack()
		for i := 0; i < l.listpack.Len(); i++ {
			elem, ok := l.listpack.Get(i)
			if !ok {
				break
			}
			if i == lastIdx {
				removed = listpackToBytes(elem)
				continue
			}
			l.appendVal(newLp, elem)
		}
		l.listpack = newLp
		return removed
	}

	return l.quicklist.RemoveLast()
}

// Set updates the value at the given index
func (l *List) Set(index int, val interface{}) {
	if l.isListpack {
		// Rebuild listpack with updated value at index
		newLp := NewListpack()
		for i := 0; i < l.listpack.Len(); i++ {
			elem, ok := l.listpack.Get(i)
			if !ok {
				break
			}
			if i == index {
				l.appendVal(newLp, val)
			} else {
				l.appendVal(newLp, elem)
			}
		}
		l.listpack = newLp
		return
	}

	l.quicklist.Set(index, val)
}

// Range returns elements within [start, stop)
func (l *List) Range(start, stop int) []interface{} {
	if l.isListpack {
		if start < 0 {
			start = 0
		}
		if stop > l.listpack.Len() {
			stop = l.listpack.Len()
		}
		if start >= stop {
			return []interface{}{}
		}
		result := make([]interface{}, 0, stop-start)
		for i := start; i < stop; i++ {
			elem, ok := l.listpack.Get(i)
			if ok {
				result = append(result, listpackToBytes(elem))
			}
		}
		return result
	}

	return l.quicklist.Range(start, stop)
}

// RemoveAllByVal removes all elements matching the predicate
func (l *List) RemoveAllByVal(expected func(interface{}) bool) int {
	if l.isListpack {
		removed := 0
		newLp := NewListpack()
		for i := 0; i < l.listpack.Len(); i++ {
			elem, ok := l.listpack.Get(i)
			if !ok {
				break
			}
			if expected(listpackToBytes(elem)) {
				removed++
				continue
			}
			l.appendVal(newLp, elem)
		}
		l.listpack = newLp
		return removed
	}

	return l.quicklist.RemoveAllByVal(expected)
}

// RemoveByVal removes at most count elements matching the predicate from left to right
func (l *List) RemoveByVal(expected func(interface{}) bool, count int) int {
	if l.isListpack {
		removed := 0
		newLp := NewListpack()
		for i := 0; i < l.listpack.Len(); i++ {
			elem, ok := l.listpack.Get(i)
			if !ok {
				break
			}
			if removed < count && expected(listpackToBytes(elem)) {
				removed++
				continue
			}
			l.appendVal(newLp, elem)
		}
		l.listpack = newLp
		return removed
	}

	return l.quicklist.RemoveByVal(expected, count)
}

// ReverseRemoveByVal removes at most count elements matching the predicate from right to left
func (l *List) ReverseRemoveByVal(expected func(interface{}) bool, count int) int {
	if l.isListpack {
		// Collect indices to remove from right to left
		total := l.listpack.Len()
		toRemove := make(map[int]bool)
		removed := 0
		for i := total - 1; i >= 0 && removed < count; i-- {
			elem, ok := l.listpack.Get(i)
			if ok && expected(listpackToBytes(elem)) {
				toRemove[i] = true
				removed++
			}
		}
		newLp := NewListpack()
		for i := 0; i < total; i++ {
			if toRemove[i] {
				continue
			}
			elem, ok := l.listpack.Get(i)
			if ok {
				l.appendVal(newLp, elem)
			}
		}
		l.listpack = newLp
		return removed
	}

	return l.quicklist.ReverseRemoveByVal(expected, count)
}

// appendVal appends a value to the given listpack, handling type conversions
func (l *List) appendVal(lp *Listpack, val interface{}) {
	switch v := val.(type) {
	case []byte:
		lp.AppendStr(string(v))
	case string:
		lp.AppendStr(v)
	case int64:
		lp.AppendInt(v)
	default:
		lp.AppendStr("")
	}
}

// GetAsQuickList returns the list as a *QuickList (for backward compatibility)
func (l *List) GetAsQuickList() *QuickList {
	if l.isListpack {
		l.convertToQuicklist()
	}
	return l.quicklist
}
