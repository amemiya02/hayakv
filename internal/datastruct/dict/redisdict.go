package dict

import (
	"github.com/amemiya02/hayakv/internal/lib/wildcard"
	"sync"
)

// Compile-time check that RedisDict implements Dict.
var _ Dict = (*RedisDict)(nil)

const (
	loadFactor = 1
	minSize    = 4
)

type entry struct {
	key  string
	val  interface{}
	next *entry
}

type hashTable struct {
	buckets []*entry
	mask    uint64
	used    int
}

// RedisDict is a single-threaded hash table with incremental rehashing,
// modelled after Redis's dict implementation.
type RedisDict struct {
	tables    [2]*hashTable
	rehashIdx int // -1 means not rehashing
	count     int
	mu        sync.RWMutex
}

// MakeRedis creates a new RedisDict with the given initial size hint.
func MakeRedis(size int) *RedisDict {
	if size < minSize {
		size = minSize
	}
	sz := nextPower(size)
	return &RedisDict{
		tables: [2]*hashTable{
			{
				buckets: make([]*entry, sz),
				mask:    uint64(sz - 1),
				used:    0,
			},
		},
		rehashIdx: -1,
		count:     0,
	}
}

// nextPower returns the smallest power of 2 >= n.
func nextPower(n int) int {
	if n <= minSize {
		return minSize
	}
	n--
	n |= n >> 1
	n |= n >> 2
	n |= n >> 4
	n |= n >> 8
	n |= n >> 16
	n |= n >> 32
	return n + 1
}

// fnv1a computes the FNV-1a hash of a string.
func fnv1a(key string) uint64 {
	const (
		offset64 = 14695981039346656037
		prime64  = 1099511628211
	)
	hash := uint64(offset64)
	for i := 0; i < len(key); i++ {
		hash ^= uint64(key[i])
		hash *= prime64
	}
	return hash
}

// isRehashing returns true if the dict is currently rehashing.
func (d *RedisDict) isRehashing() bool {
	return d.rehashIdx != -1
}

// startRehashIfNeeded begins a rehash if the load factor is exceeded.
func (d *RedisDict) startRehashIfNeeded() {
	if d.isRehashing() {
		return
	}
	ht := d.tables[0]
	if ht.used == 0 {
		// Avoid division by zero; start rehash if size is 0 (shouldn't happen with minSize)
		return
	}
	if float64(ht.used)/float64(len(ht.buckets)) >= loadFactor {
		d.startRehash(len(ht.buckets) * 2)
	}
}

// startRehash initiates rehashing to a new table of the given size.
func (d *RedisDict) startRehash(newSize int) {
	if d.isRehashing() {
		return
	}
	if newSize < d.tables[0].used {
		newSize = d.tables[0].used
	}
	sz := nextPower(newSize)
	d.tables[1] = &hashTable{
		buckets: make([]*entry, sz),
		mask:    uint64(sz - 1),
		used:    0,
	}
	d.rehashIdx = 0
}

// rehashStep migrates one bucket from ht[0] to ht[1].
func (d *RedisDict) rehashStep() {
	if !d.isRehashing() {
		return
	}
	ht0 := d.tables[0]
	ht1 := d.tables[1]

	// Find a non-empty bucket
	for d.rehashIdx < len(ht0.buckets) {
		e := ht0.buckets[d.rehashIdx]
		if e == nil {
			d.rehashIdx++
			continue
		}
		// Move all entries in this bucket to ht[1]
		ht0.buckets[d.rehashIdx] = nil
		for e != nil {
			next := e.next
			idx := fnv1a(e.key) & ht1.mask
			e.next = ht1.buckets[idx]
			ht1.buckets[idx] = e
			ht1.used++
			e = next
		}
		d.rehashIdx++
		return
	}

	// Rehash complete
	d.tables[0] = d.tables[1]
	d.tables[1] = nil
	d.rehashIdx = -1
}

// bucketIndex returns the bucket index and table for a key.
func (d *RedisDict) bucketIndex(key string) (ht *hashTable, idx uint64) {
	h := fnv1a(key)
	if d.isRehashing() {
		ht0 := d.tables[0]
		if int(h&ht0.mask) >= d.rehashIdx {
			// Still in ht[0] range
			return ht0, h & ht0.mask
		}
		// Already migrated, use ht[1]
		return d.tables[1], h & d.tables[1].mask
	}
	ht = d.tables[0]
	return ht, h & ht.mask
}

// findEntry finds an entry by key in the given bucket chain.
func findEntry(bucket *entry, key string) *entry {
	for e := bucket; e != nil; e = e.next {
		if e.key == key {
			return e
		}
	}
	return nil
}

// Get returns the binding value and whether the key exists.
func (d *RedisDict) Get(key string) (val interface{}, exists bool) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	ht, idx := d.bucketIndex(key)
	e := findEntry(ht.buckets[idx], key)
	if e != nil {
		return e.val, true
	}
	return nil, false
}

// Len returns the number of entries in the dict.
func (d *RedisDict) Len() int {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.count
}

// Put inserts or updates a key-value pair. Returns 1 if new, 0 if updated.
func (d *RedisDict) Put(key string, val interface{}) (result int) {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.startRehashIfNeeded()
	d.rehashStep()

	ht, idx := d.bucketIndex(key)
	e := findEntry(ht.buckets[idx], key)
	if e != nil {
		e.val = val
		return 0
	}

	// Insert new entry at head of bucket
	ht.buckets[idx] = &entry{
		key:  key,
		val:  val,
		next: ht.buckets[idx],
	}
	ht.used++
	d.count++
	return 1
}

// PutIfAbsent inserts only if key doesn't exist. Returns 1 if inserted, 0 otherwise.
func (d *RedisDict) PutIfAbsent(key string, val interface{}) (result int) {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.startRehashIfNeeded()
	d.rehashStep()

	ht, idx := d.bucketIndex(key)
	e := findEntry(ht.buckets[idx], key)
	if e != nil {
		return 0
	}

	ht.buckets[idx] = &entry{
		key:  key,
		val:  val,
		next: ht.buckets[idx],
	}
	ht.used++
	d.count++
	return 1
}

// PutIfExists updates only if key exists. Returns 1 if updated, 0 otherwise.
func (d *RedisDict) PutIfExists(key string, val interface{}) (result int) {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.startRehashIfNeeded()
	d.rehashStep()

	ht, idx := d.bucketIndex(key)
	e := findEntry(ht.buckets[idx], key)
	if e != nil {
		e.val = val
		return 1
	}
	return 0
}

// Remove removes the key and returns the value and count of deleted entries.
func (d *RedisDict) Remove(key string) (val interface{}, result int) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.isRehashing() {
		d.rehashStep()
	}

	// Try both tables during rehashing
	for i := 0; i < 2; i++ {
		ht := d.tables[i]
		if ht == nil {
			continue
		}
		h := fnv1a(key)
		idx := h & ht.mask
		var prev *entry
		for e := ht.buckets[idx]; e != nil; e = e.next {
			if e.key == key {
				if prev == nil {
					ht.buckets[idx] = e.next
				} else {
					prev.next = e.next
				}
				ht.used--
				d.count--
				return e.val, 1
			}
			prev = e
		}
		if !d.isRehashing() {
			break
		}
	}
	return nil, 0
}

// Clear removes all entries and resets the dict.
func (d *RedisDict) Clear() {
	d.mu.Lock()
	defer d.mu.Unlock()

	sz := minSize
	d.tables = [2]*hashTable{
		{
			buckets: make([]*entry, sz),
			mask:    uint64(sz - 1),
			used:    0,
		},
	}
	d.rehashIdx = -1
	d.count = 0
}

// ForEach traverses all entries, calling consumer for each.
func (d *RedisDict) ForEach(consumer Consumer) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	for i := 0; i < 2; i++ {
		ht := d.tables[i]
		if ht == nil {
			continue
		}
		for _, bucket := range ht.buckets {
			for e := bucket; e != nil; e = e.next {
				if !consumer(e.key, e.val) {
					return
				}
			}
		}
		if !d.isRehashing() {
			break
		}
	}
}

// Keys returns all keys in the dict.
func (d *RedisDict) Keys() []string {
	keys := make([]string, 0, d.Len())
	d.ForEach(func(key string, val interface{}) bool {
		keys = append(keys, key)
		return true
	})
	return keys
}

// RandomKeys returns limit random keys (may contain duplicates).
func (d *RedisDict) RandomKeys(limit int) []string {
	d.mu.RLock()
	defer d.mu.RUnlock()

	result := make([]string, 0, limit)
	// Collect from all tables
	for i := 0; i < 2; i++ {
		ht := d.tables[i]
		if ht == nil {
			continue
		}
		for _, bucket := range ht.buckets {
			for e := bucket; e != nil; e = e.next {
				result = append(result, e.key)
				if len(result) >= limit {
					return result
				}
			}
		}
		if !d.isRehashing() {
			break
		}
	}
	return result
}

// RandomDistinctKeys returns limit distinct random keys.
func (d *RedisDict) RandomDistinctKeys(limit int) []string {
	d.mu.RLock()
	defer d.mu.RUnlock()

	seen := make(map[string]struct{}, limit)
	result := make([]string, 0, limit)
	for i := 0; i < 2; i++ {
		ht := d.tables[i]
		if ht == nil {
			continue
		}
		for _, bucket := range ht.buckets {
			for e := bucket; e != nil; e = e.next {
				if _, ok := seen[e.key]; !ok {
					seen[e.key] = struct{}{}
					result = append(result, e.key)
					if len(result) >= limit {
						return result
					}
				}
			}
		}
		if !d.isRehashing() {
			break
		}
	}
	return result
}

// bitReverse reverses the lower `bits` bits of `val`.
func bitReverse(val uint64, bits uint) uint64 {
	var result uint64
	for i := uint(0); i < bits; i++ {
		if val&(1<<i) != 0 {
			result |= 1 << (bits - 1 - i)
		}
	}
	return result
}

// reverseBits reverses all 64 bits of val.
func reverseBits(val uint64) uint64 {
	val = ((val & 0x5555555555555555) << 1) | ((val & 0xAAAAAAAAAAAAAAAA) >> 1)
	val = ((val & 0x3333333333333333) << 2) | ((val & 0xCCCCCCCCCCCCCCCC) >> 2)
	val = ((val & 0x0F0F0F0F0F0F0F0F) << 4) | ((val & 0xF0F0F0F0F0F0F0F0) >> 4)
	val = ((val & 0x00FF00FF00FF00FF) << 8) | ((val & 0xFF00FF00FF00FF00) >> 8)
	val = ((val & 0x0000FFFF0000FFFF) << 16) | ((val & 0xFFFF0000FFFF0000) >> 16)
	val = (val << 32) | (val >> 32)
	return val
}

// --- Locking methods (no-ops for RedisDict) ---
// The outer lockedEngine (for goroutine net) or single-threaded eventloop
// provides the concurrency guarantee, so these delegate to the plain methods.

func (d *RedisDict) GetWithLock(key string) (interface{}, bool)           { return d.Get(key) }
func (d *RedisDict) PutWithLock(key string, val interface{}) int          { return d.Put(key, val) }
func (d *RedisDict) PutIfAbsentWithLock(key string, val interface{}) int  { return d.PutIfAbsent(key, val) }
func (d *RedisDict) PutIfExistsWithLock(key string, val interface{}) int  { return d.PutIfExists(key, val) }
func (d *RedisDict) RemoveWithLock(key string) (interface{}, int)         { return d.Remove(key) }
func (d *RedisDict) RWLocks(writeKeys []string, readKeys []string)        {}
func (d *RedisDict) RWUnLocks(writeKeys []string, readKeys []string)      {}

// DictScan implements cursor-based scanning. The cursor represents
// the next table index to scan. Returns (keys, nextCursor) where
// nextCursor==0 means the scan is complete.
func (d *RedisDict) DictScan(cursor int, count int, pattern string) ([][]byte, int) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	result := make([][]byte, 0)

	size := d.count
	if size == 0 {
		return result, 0
	}

	// Fast path: return all keys at once
	if pattern == "*" && count >= size {
		for i := 0; i < 2; i++ {
			ht := d.tables[i]
			if ht == nil {
				continue
			}
			for _, bucket := range ht.buckets {
				for e := bucket; e != nil; e = e.next {
					result = append(result, []byte(e.key))
				}
			}
			if !d.isRehashing() {
				break
			}
		}
		return result, 0
	}

	// Compile pattern
	var matchKey = func(key string) bool { return true }
	if pattern != "*" {
		pat, err := wildcard.CompilePattern(pattern)
		if err != nil {
			return result, -1
		}
		matchKey = pat.IsMatch
	}

	ht0Size := len(d.tables[0].buckets)
	ht1Size := 0
	if d.tables[1] != nil {
		ht1Size = len(d.tables[1].buckets)
	}

	// Decode cursor: lower bits = ht[0] index, upper bit = which table
	// For non-rehashing: cursor is just the ht[0] bucket index
	// For rehashing: cursor encodes (tableIdx * ht0Size) + bucketIdx
	var ht0Idx, ht1Idx int
	if d.isRehashing() {
		// During rehash, cursor ranges: [0, ht0Size) for ht[0], [ht0Size, ht0Size+ht1Size) for ht[1]
		if cursor < ht0Size {
			ht0Idx = cursor
			ht1Idx = 0
		} else {
			ht0Idx = ht0Size // skip ht[0]
			ht1Idx = cursor - ht0Size
		}
	} else {
		ht0Idx = cursor
		ht1Idx = 0
	}

	// Scan ht[0] from ht0Idx
	for ht0Idx < ht0Size && len(result) < count {
		for e := d.tables[0].buckets[ht0Idx]; e != nil; e = e.next {
			if pattern == "*" || matchKey(e.key) {
				result = append(result, []byte(e.key))
			}
		}
		ht0Idx++
		if len(result) >= count {
			break
		}
	}

	// Scan ht[1] from ht1Idx (if rehashing)
	if d.isRehashing() && ht1Size > 0 {
		for ht1Idx < ht1Size && len(result) < count {
			for e := d.tables[1].buckets[ht1Idx]; e != nil; e = e.next {
				if pattern == "*" || matchKey(e.key) {
					result = append(result, []byte(e.key))
				}
			}
			ht1Idx++
			if len(result) >= count {
				break
			}
		}
	}

	// Determine next cursor
	if d.isRehashing() {
		if ht1Idx >= ht1Size && ht0Idx >= ht0Size {
			return result, 0
		}
		if ht0Idx < ht0Size {
			return result, ht0Idx
		}
		return result, ht0Size + ht1Idx
	}

	if ht0Idx >= ht0Size {
		return result, 0
	}
	return result, ht0Idx
}
