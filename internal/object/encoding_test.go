package object

import (
	"testing"
)

func TestHashNew(t *testing.T) {
	h := NewHash()
	if h.Len() != 0 {
		t.Fatalf("new hash should have length 0, got %d", h.Len())
	}
	if !h.IsListpack() {
		t.Fatal("new hash should be listpack encoding")
	}
}

func TestHashPutGet(t *testing.T) {
	h := NewHash()

	// Put values
	result := h.Put("field1", []byte("value1"))
	if result != 1 {
		t.Fatalf("expected 1 for new field, got %d", result)
	}

	result = h.Put("field2", []byte("value2"))
	if result != 1 {
		t.Fatalf("expected 1 for new field, got %d", result)
	}

	// Get values
	val, ok := h.Get("field1")
	if !ok {
		t.Fatal("expected to find field1")
	}
	// Value is stored as string in listpack
	strVal, ok := val.(string)
	if !ok {
		t.Fatalf("expected string, got %T", val)
	}
	if strVal != "value1" {
		t.Fatalf("expected 'value1', got %q", strVal)
	}

	val, ok = h.Get("field2")
	if !ok {
		t.Fatal("expected to find field2")
	}
	strVal, ok = val.(string)
	if !ok {
		t.Fatalf("expected string, got %T", val)
	}
	if strVal != "value2" {
		t.Fatalf("expected 'value2', got %q", strVal)
	}

	// Get non-existent field
	_, ok = h.Get("field3")
	if ok {
		t.Fatal("should not find field3")
	}
}

func TestHashPutUpdate(t *testing.T) {
	h := NewHash()

	// Put initial value
	h.Put("field1", []byte("value1"))

	// Update value
	result := h.Put("field1", []byte("value2"))
	if result != 0 {
		t.Fatalf("expected 0 for update, got %d", result)
	}

	// Verify update
	val, ok := h.Get("field1")
	if !ok {
		t.Fatal("expected to find field1")
	}
	strVal, ok := val.(string)
	if !ok {
		t.Fatalf("expected string, got %T", val)
	}
	if strVal != "value2" {
		t.Fatalf("expected 'value2', got %q", strVal)
	}
}

func TestHashRemove(t *testing.T) {
	h := NewHash()

	// Put values
	h.Put("field1", []byte("value1"))
	h.Put("field2", []byte("value2"))

	// Remove field
	_, removed := h.Remove("field1")
	if removed != 1 {
		t.Fatalf("expected 1 for removed, got %d", removed)
	}

	// Verify removed
	_, ok := h.Get("field1")
	if ok {
		t.Fatal("should not find field1 after removal")
	}

	// Verify other field still exists
	val, ok := h.Get("field2")
	if !ok {
		t.Fatal("expected to find field2")
	}
	strVal, ok := val.(string)
	if !ok {
		t.Fatalf("expected string, got %T", val)
	}
	if strVal != "value2" {
		t.Fatalf("expected 'value2', got %q", strVal)
	}

	// Remove non-existent field
	_, removed = h.Remove("field3")
	if removed != 0 {
		t.Fatalf("expected 0 for non-existent field, got %d", removed)
	}
}

func TestHashLen(t *testing.T) {
	h := NewHash()

	if h.Len() != 0 {
		t.Fatalf("expected length 0, got %d", h.Len())
	}

	h.Put("field1", []byte("value1"))
	if h.Len() != 1 {
		t.Fatalf("expected length 1, got %d", h.Len())
	}

	h.Put("field2", []byte("value2"))
	if h.Len() != 2 {
		t.Fatalf("expected length 2, got %d", h.Len())
	}

	h.Remove("field1")
	if h.Len() != 1 {
		t.Fatalf("expected length 1, got %d", h.Len())
	}
}

func TestHashForEach(t *testing.T) {
	h := NewHash()

	h.Put("field1", []byte("value1"))
	h.Put("field2", []byte("value2"))
	h.Put("field3", []byte("value3"))

	// Collect all fields
	fields := make(map[string]string)
	h.ForEach(func(field string, value interface{}) bool {
		// Value is stored as string in listpack
		if strVal, ok := value.(string); ok {
			fields[field] = strVal
		} else if bytesVal, ok := value.([]byte); ok {
			fields[field] = string(bytesVal)
		}
		return true
	})

	if len(fields) != 3 {
		t.Fatalf("expected 3 fields, got %d", len(fields))
	}
	if fields["field1"] != "value1" {
		t.Fatalf("expected 'value1' for field1, got %q", fields["field1"])
	}
	if fields["field2"] != "value2" {
		t.Fatalf("expected 'value2' for field2, got %q", fields["field2"])
	}
	if fields["field3"] != "value3" {
		t.Fatalf("expected 'value3' for field3, got %q", fields["field3"])
	}
}

func TestHashPutIfAbsent(t *testing.T) {
	h := NewHash()

	// Put initial value
	result := h.PutIfAbsent("field1", []byte("value1"))
	if result != 1 {
		t.Fatalf("expected 1 for new field, got %d", result)
	}

	// Try to put again (should not update)
	result = h.PutIfAbsent("field1", []byte("value2"))
	if result != 0 {
		t.Fatalf("expected 0 for existing field, got %d", result)
	}

	// Verify original value
	val, ok := h.Get("field1")
	if !ok {
		t.Fatal("expected to find field1")
	}
	strVal, ok := val.(string)
	if !ok {
		t.Fatalf("expected string, got %T", val)
	}
	if strVal != "value1" {
		t.Fatalf("expected 'value1', got %q", strVal)
	}
}

func TestHashLargeList(t *testing.T) {
	h := NewHash()

	// Add many fields
	for i := 0; i < 100; i++ {
		field := "field" + string(rune('0'+i%10))
		value := "value" + string(rune('0'+i%10))
		h.Put(field, []byte(value))
	}

	// Verify length
	if h.Len() != 10 {
		t.Fatalf("expected length 10 (unique fields), got %d", h.Len())
	}

	// Verify all fields exist
	for i := 0; i < 10; i++ {
		field := "field" + string(rune('0'+i))
		val, ok := h.Get(field)
		if !ok {
			t.Fatalf("expected to find %s", field)
		}
		strVal, ok := val.(string)
		if !ok {
			t.Fatalf("expected string, got %T", val)
		}
		expected := "value" + string(rune('0'+i))
		if strVal != expected {
			t.Fatalf("expected %q for %s, got %q", expected, field, strVal)
		}
	}
}

func TestHashConversion(t *testing.T) {
	h := NewHash()

	// Add enough fields to trigger conversion
	for i := 0; i < 200; i++ {
		field := "field" + string(rune('0'+i%10)) + string(rune('0'+i/10))
		value := "value" + string(rune('0'+i%10)) + string(rune('0'+i/10))
		h.Put(field, []byte(value))
	}

	// Should have converted to hashtable
	if h.IsListpack() {
		t.Fatal("expected to convert to hashtable")
	}

	// Verify all fields still exist
	for i := 0; i < 200; i++ {
		field := "field" + string(rune('0'+i%10)) + string(rune('0'+i/10))
		val, ok := h.Get(field)
		if !ok {
			t.Fatalf("expected to find %s", field)
		}
		// After conversion, value is stored as []byte
		bytesVal, ok := val.([]byte)
		if !ok {
			t.Fatalf("expected []byte, got %T", val)
		}
		expected := "value" + string(rune('0'+i%10)) + string(rune('0'+i/10))
		if string(bytesVal) != expected {
			t.Fatalf("expected %q for %s, got %q", expected, field, string(bytesVal))
		}
	}
}

func TestSetNew(t *testing.T) {
	s := NewSet()
	if s.Len() != 0 {
		t.Fatalf("new set should have length 0, got %d", s.Len())
	}
	if s.Encoding() != "intset" {
		t.Fatalf("new set should be intset encoding, got %s", s.Encoding())
	}
}

func TestSetAddInt(t *testing.T) {
	s := NewSet()

	// Add integers
	result := s.Add("42")
	if result != 1 {
		t.Fatalf("expected 1 for new member, got %d", result)
	}

	result = s.Add("10")
	if result != 1 {
		t.Fatalf("expected 1 for new member, got %d", result)
	}

	// Add duplicate
	result = s.Add("42")
	if result != 0 {
		t.Fatalf("expected 0 for duplicate, got %d", result)
	}

	// Verify length
	if s.Len() != 2 {
		t.Fatalf("expected length 2, got %d", s.Len())
	}

	// Verify members exist
	if !s.Has("42") {
		t.Fatal("expected to have 42")
	}
	if !s.Has("10") {
		t.Fatal("expected to have 10")
	}
	if s.Has("99") {
		t.Fatal("should not have 99")
	}
}

func TestSetAddString(t *testing.T) {
	s := NewSet()

	// Add string (should convert to listpack)
	result := s.Add("hello")
	if result != 1 {
		t.Fatalf("expected 1 for new member, got %d", result)
	}

	// Should now be listpack encoding
	if s.Encoding() != "listpack" {
		t.Fatalf("expected listpack encoding, got %s", s.Encoding())
	}

	result = s.Add("world")
	if result != 1 {
		t.Fatalf("expected 1 for new member, got %d", result)
	}

	// Verify length
	if s.Len() != 2 {
		t.Fatalf("expected length 2, got %d", s.Len())
	}

	// Verify members exist
	if !s.Has("hello") {
		t.Fatal("expected to have hello")
	}
	if !s.Has("world") {
		t.Fatal("expected to have world")
	}
}

func TestSetRemove(t *testing.T) {
	s := NewSet()

	// Add members
	s.Add("42")
	s.Add("10")
	s.Add("100")

	// Remove member
	result := s.Remove("42")
	if result != 1 {
		t.Fatalf("expected 1 for removed, got %d", result)
	}

	// Verify removed
	if s.Has("42") {
		t.Fatal("should not have 42 after removal")
	}

	// Verify other members still exist
	if !s.Has("10") {
		t.Fatal("expected to have 10")
	}
	if !s.Has("100") {
		t.Fatal("expected to have 100")
	}

	// Remove non-existent member
	result = s.Remove("99")
	if result != 0 {
		t.Fatalf("expected 0 for non-existent, got %d", result)
	}
}

func TestSetForEach(t *testing.T) {
	s := NewSet()

	s.Add("10")
	s.Add("42")
	s.Add("100")

	// Collect all members
	members := make(map[string]bool)
	s.ForEach(func(member string) bool {
		members[member] = true
		return true
	})

	if len(members) != 3 {
		t.Fatalf("expected 3 members, got %d", len(members))
	}
	if !members["10"] {
		t.Fatal("expected to have 10")
	}
	if !members["42"] {
		t.Fatal("expected to have 42")
	}
	if !members["100"] {
		t.Fatal("expected to have 100")
	}
}

func TestSetIntsetToListpack(t *testing.T) {
	s := NewSet()

	// Add integers to trigger intset->listpack conversion
	// When we add a non-int, it converts to listpack
	// But if we add more than 128, it will convert to hashtable
	for i := 0; i < 100; i++ {
		s.Add(formatInt64(int64(i)))
	}

	// Should still be intset (100 < 512)
	if s.Encoding() != "intset" {
		t.Fatalf("expected intset encoding, got %s", s.Encoding())
	}

	// Add more to trigger intset->listpack
	for i := 100; i < 513; i++ {
		s.Add(formatInt64(int64(i)))
	}

	// Should have converted to listpack, then immediately to hashtable
	// because 513 > 128 (listpack threshold)
	if s.Encoding() != "hashtable" {
		t.Fatalf("expected hashtable encoding, got %s", s.Encoding())
	}

	// Verify all members exist
	for i := 0; i < 513; i++ {
		if !s.Has(formatInt64(int64(i))) {
			t.Fatalf("expected to have %d", i)
		}
	}
}

func TestSetListpackToHashtable(t *testing.T) {
	s := NewSet()

	// Add enough members to trigger conversion
	for i := 0; i < 200; i++ {
		s.Add("member" + formatInt64(int64(i)))
	}

	// Should have converted to hashtable
	if s.Encoding() != "hashtable" {
		t.Fatalf("expected hashtable encoding, got %s", s.Encoding())
	}

	// Verify all members exist
	for i := 0; i < 200; i++ {
		member := "member" + formatInt64(int64(i))
		if !s.Has(member) {
			t.Fatalf("expected to have %s", member)
		}
	}
}

func TestZSetNew(t *testing.T) {
	z := NewZSet()
	if z.Len() != 0 {
		t.Fatalf("new zset should have length 0, got %d", z.Len())
	}
	if !z.IsListpack() {
		t.Fatal("new zset should be listpack encoding")
	}
}

func TestZSetAddGet(t *testing.T) {
	z := NewZSet()

	// Add members
	result := z.Add("member1", 1.0)
	if !result {
		t.Fatal("expected true for new member")
	}

	result = z.Add("member2", 2.0)
	if !result {
		t.Fatal("expected true for new member")
	}

	// Get scores
	score, ok := z.Get("member1")
	if !ok {
		t.Fatal("expected to find member1")
	}
	if score != 1.0 {
		t.Fatalf("expected score 1.0, got %f", score)
	}

	score, ok = z.Get("member2")
	if !ok {
		t.Fatal("expected to find member2")
	}
	if score != 2.0 {
		t.Fatalf("expected score 2.0, got %f", score)
	}

	// Get non-existent member
	_, ok = z.Get("member3")
	if ok {
		t.Fatal("should not find member3")
	}
}

func TestZSetAddUpdate(t *testing.T) {
	z := NewZSet()

	// Add member
	z.Add("member1", 1.0)

	// Update score
	result := z.Add("member1", 2.0)
	if result {
		t.Fatal("expected false for update")
	}

	// Verify update
	score, ok := z.Get("member1")
	if !ok {
		t.Fatal("expected to find member1")
	}
	if score != 2.0 {
		t.Fatalf("expected score 2.0, got %f", score)
	}
}

func TestZSetRemove(t *testing.T) {
	z := NewZSet()

	// Add members
	z.Add("member1", 1.0)
	z.Add("member2", 2.0)
	z.Add("member3", 3.0)

	// Remove member
	result := z.Remove("member2")
	if !result {
		t.Fatal("expected true for removed")
	}

	// Verify removed
	_, ok := z.Get("member2")
	if ok {
		t.Fatal("should not find member2 after removal")
	}

	// Verify other members still exist
	score, ok := z.Get("member1")
	if !ok || score != 1.0 {
		t.Fatal("expected to have member1 with score 1.0")
	}
	score, ok = z.Get("member3")
	if !ok || score != 3.0 {
		t.Fatal("expected to have member3 with score 3.0")
	}

	// Remove non-existent member
	result = z.Remove("member4")
	if result {
		t.Fatal("expected false for non-existent")
	}
}

func TestZSetLen(t *testing.T) {
	z := NewZSet()

	if z.Len() != 0 {
		t.Fatalf("expected length 0, got %d", z.Len())
	}

	z.Add("member1", 1.0)
	if z.Len() != 1 {
		t.Fatalf("expected length 1, got %d", z.Len())
	}

	z.Add("member2", 2.0)
	if z.Len() != 2 {
		t.Fatalf("expected length 2, got %d", z.Len())
	}

	z.Remove("member1")
	if z.Len() != 1 {
		t.Fatalf("expected length 1, got %d", z.Len())
	}
}

func TestZSetForEach(t *testing.T) {
	z := NewZSet()

	z.Add("member1", 1.0)
	z.Add("member2", 2.0)
	z.Add("member3", 3.0)

	// Collect all members
	members := make(map[string]float64)
	z.ForEach(func(member string, score float64) bool {
		members[member] = score
		return true
	})

	if len(members) != 3 {
		t.Fatalf("expected 3 members, got %d", len(members))
	}
	if members["member1"] != 1.0 {
		t.Fatalf("expected score 1.0 for member1, got %f", members["member1"])
	}
	if members["member2"] != 2.0 {
		t.Fatalf("expected score 2.0 for member2, got %f", members["member2"])
	}
	if members["member3"] != 3.0 {
		t.Fatalf("expected score 3.0 for member3, got %f", members["member3"])
	}
}

func TestZSetConversion(t *testing.T) {
	z := NewZSet()

	// Add enough members to trigger conversion
	for i := 0; i < 200; i++ {
		member := "member" + formatInt64(int64(i))
		z.Add(member, float64(i))
	}

	// Should have converted to skiplist
	if z.IsListpack() {
		t.Fatal("expected to convert to skiplist")
	}

	// Verify all members still exist
	for i := 0; i < 200; i++ {
		member := "member" + formatInt64(int64(i))
		score, ok := z.Get(member)
		if !ok {
			t.Fatalf("expected to find %s", member)
		}
		if score != float64(i) {
			t.Fatalf("expected score %f for %s, got %f", float64(i), member, score)
		}
	}
}

func TestListNew(t *testing.T) {
	l := NewList()
	if l.Len() != 0 {
		t.Fatalf("new list should have length 0, got %d", l.Len())
	}
	if !l.IsListpack() {
		t.Fatal("new list should be listpack encoding")
	}
}

func TestListAddGet(t *testing.T) {
	l := NewList()

	// Add values
	l.Add([]byte("value1"))
	l.Add([]byte("value2"))
	l.Add([]byte("value3"))

	// Get values
	val, ok := l.Get(0)
	if !ok {
		t.Fatal("expected to find value at index 0")
	}
	if string(val.([]byte)) != "value1" {
		t.Fatalf("expected 'value1', got %q", val)
	}

	val, ok = l.Get(1)
	if !ok {
		t.Fatal("expected to find value at index 1")
	}
	if string(val.([]byte)) != "value2" {
		t.Fatalf("expected 'value2', got %q", val)
	}

	val, ok = l.Get(2)
	if !ok {
		t.Fatal("expected to find value at index 2")
	}
	if string(val.([]byte)) != "value3" {
		t.Fatalf("expected 'value3', got %q", val)
	}

	// Get out of bounds
	_, ok = l.Get(3)
	if ok {
		t.Fatal("should not find value at index 3")
	}
}

func TestListLen(t *testing.T) {
	l := NewList()

	if l.Len() != 0 {
		t.Fatalf("expected length 0, got %d", l.Len())
	}

	l.Add([]byte("value1"))
	if l.Len() != 1 {
		t.Fatalf("expected length 1, got %d", l.Len())
	}

	l.Add([]byte("value2"))
	if l.Len() != 2 {
		t.Fatalf("expected length 2, got %d", l.Len())
	}
}

func TestListForEach(t *testing.T) {
	l := NewList()

	l.Add([]byte("value1"))
	l.Add([]byte("value2"))
	l.Add([]byte("value3"))

	// Collect all values
	var values []string
	l.ForEach(func(i int, val interface{}) bool {
		values = append(values, string(val.([]byte)))
		return true
	})

	if len(values) != 3 {
		t.Fatalf("expected 3 values, got %d", len(values))
	}
	if values[0] != "value1" {
		t.Fatalf("expected 'value1', got %q", values[0])
	}
	if values[1] != "value2" {
		t.Fatalf("expected 'value2', got %q", values[1])
	}
	if values[2] != "value3" {
		t.Fatalf("expected 'value3', got %q", values[2])
	}
}

func TestListConversion(t *testing.T) {
	l := NewList()

	// Add enough values to trigger conversion
	for i := 0; i < 200; i++ {
		l.Add([]byte("value" + formatInt64(int64(i))))
	}

	// Should have converted to quicklist
	if l.IsListpack() {
		t.Fatal("expected to convert to quicklist")
	}

	// Verify all values still exist
	for i := 0; i < 200; i++ {
		val, ok := l.Get(i)
		if !ok {
			t.Fatalf("expected to find value at index %d", i)
		}
		expected := "value" + formatInt64(int64(i))
		if string(val.([]byte)) != expected {
			t.Fatalf("expected %q at index %d, got %q", expected, i, string(val.([]byte)))
		}
	}
}
