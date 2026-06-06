package object

import (
	"strconv"

	"github.com/amemiya02/hayakv/internal/datastruct/dict"
	"github.com/amemiya02/hayakv/internal/datastruct/list"
	"github.com/amemiya02/hayakv/internal/datastruct/sortedset"
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
	listpack    *Listpack
	hashtable   dict.Dict
	isListpack  bool
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

// ZSet represents a sorted set object that can be encoded as listpack or skiplist
type ZSet struct {
	listpack  *Listpack
	skiplist  *SortedSet
	isListpack bool
}

// NewZSet creates a new sorted set object
func NewZSet() *ZSet {
	return &ZSet{
		listpack:  NewListpack(),
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

// List represents a list object that can be encoded as listpack or quicklist
type List struct {
	listpack  *Listpack
	quicklist *QuickList
	isListpack bool
}

// NewList creates a new list object
func NewList() *List {
	return &List{
		listpack:  NewListpack(),
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

// Get returns the value at the given index
func (l *List) Get(index int) (interface{}, bool) {
	if l.isListpack {
		return l.listpack.Get(index)
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
		l.listpack.Range(fn)
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

// GetAsQuickList returns the list as a *QuickList (for backward compatibility)
func (l *List) GetAsQuickList() *QuickList {
	if l.isListpack {
		l.convertToQuicklist()
	}
	return l.quicklist
}
