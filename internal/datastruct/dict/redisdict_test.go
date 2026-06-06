package dict

import (
	"strconv"
	"testing"
)

func TestRedisDict_PutGet(t *testing.T) {
	d := MakeRedis(0)
	ret := d.Put("key1", "val1")
	if ret != 1 {
		t.Errorf("expected 1, got %d", ret)
	}
	ret = d.Put("key1", "val2")
	if ret != 0 {
		t.Errorf("expected 0, got %d", ret)
	}
	val, ok := d.Get("key1")
	if !ok || val != "val2" {
		t.Errorf("expected val2, got %v", val)
	}
	_, ok = d.Get("nonexistent")
	if ok {
		t.Error("expected key not found")
	}
}

func TestRedisDict_Len(t *testing.T) {
	d := MakeRedis(0)
	if d.Len() != 0 {
		t.Errorf("expected 0, got %d", d.Len())
	}
	d.Put("a", 1)
	d.Put("b", 2)
	d.Put("c", 3)
	if d.Len() != 3 {
		t.Errorf("expected 3, got %d", d.Len())
	}
}

func TestRedisDict_PutIfAbsent(t *testing.T) {
	d := MakeRedis(0)
	ret := d.PutIfAbsent("k", "v1")
	if ret != 1 {
		t.Errorf("expected 1, got %d", ret)
	}
	ret = d.PutIfAbsent("k", "v2")
	if ret != 0 {
		t.Errorf("expected 0, got %d", ret)
	}
	val, _ := d.Get("k")
	if val != "v1" {
		t.Errorf("expected v1, got %v", val)
	}
}

func TestRedisDict_PutIfExists(t *testing.T) {
	d := MakeRedis(0)
	ret := d.PutIfExists("k", "v1")
	if ret != 0 {
		t.Errorf("expected 0, got %d", ret)
	}
	d.Put("k", "v1")
	ret = d.PutIfExists("k", "v2")
	if ret != 1 {
		t.Errorf("expected 1, got %d", ret)
	}
	val, _ := d.Get("k")
	if val != "v2" {
		t.Errorf("expected v2, got %v", val)
	}
}

func TestRedisDict_Remove(t *testing.T) {
	d := MakeRedis(0)
	d.Put("k", "v")
	val, ret := d.Remove("k")
	if ret != 1 || val != "v" {
		t.Errorf("expected 1 and v, got %d and %v", ret, val)
	}
	_, ok := d.Get("k")
	if ok {
		t.Error("expected key not found after remove")
	}
	_, ret = d.Remove("k")
	if ret != 0 {
		t.Errorf("expected 0, got %d", ret)
	}
}

func TestRedisDict_Clear(t *testing.T) {
	d := MakeRedis(0)
	for i := 0; i < 100; i++ {
		d.Put("k"+strconv.Itoa(i), i)
	}
	if d.Len() != 100 {
		t.Errorf("expected 100, got %d", d.Len())
	}
	d.Clear()
	if d.Len() != 0 {
		t.Errorf("expected 0 after clear, got %d", d.Len())
	}
}

func TestRedisDict_ForEach(t *testing.T) {
	d := MakeRedis(0)
	for i := 0; i < 10; i++ {
		d.Put("k"+strconv.Itoa(i), i)
	}
	count := 0
	d.ForEach(func(key string, val interface{}) bool {
		count++
		return true
	})
	if count != 10 {
		t.Errorf("expected 10, got %d", count)
	}
}

func TestRedisDict_Keys(t *testing.T) {
	d := MakeRedis(0)
	for i := 0; i < 10; i++ {
		d.Put("k"+strconv.Itoa(i), i)
	}
	keys := d.Keys()
	if len(keys) != 10 {
		t.Errorf("expected 10, got %d", len(keys))
	}
}

func TestRedisDict_RandomKeys(t *testing.T) {
	d := MakeRedis(0)
	for i := 0; i < 10; i++ {
		d.Put("k"+strconv.Itoa(i), i)
	}
	keys := d.RandomKeys(5)
	if len(keys) != 5 {
		t.Errorf("expected 5, got %d", len(keys))
	}
}

func TestRedisDict_RandomDistinctKeys(t *testing.T) {
	d := MakeRedis(0)
	for i := 0; i < 10; i++ {
		d.Put("k"+strconv.Itoa(i), i)
	}
	keys := d.RandomDistinctKeys(5)
	if len(keys) != 5 {
		t.Errorf("expected 5, got %d", len(keys))
	}
	seen := make(map[string]struct{})
	for _, k := range keys {
		if _, ok := seen[k]; ok {
			t.Errorf("duplicate key: %s", k)
		}
		seen[k] = struct{}{}
	}
}

func TestRedisDict_Rehash(t *testing.T) {
	d := MakeRedis(4)
	// Insert enough to trigger rehash
	for i := 0; i < 100; i++ {
		d.Put("k"+strconv.Itoa(i), i)
	}
	if d.Len() != 100 {
		t.Errorf("expected 100, got %d", d.Len())
	}
	// Verify all keys can be retrieved
	for i := 0; i < 100; i++ {
		val, ok := d.Get("k" + strconv.Itoa(i))
		if !ok || val != i {
			t.Errorf("expected %d, got %v for key k%d", i, val, i)
		}
	}
}

func TestRedisDict_RemoveDuringRehash(t *testing.T) {
	d := MakeRedis(4)
	for i := 0; i < 50; i++ {
		d.Put("k"+strconv.Itoa(i), i)
	}
	// Force rehash
	d.startRehashIfNeeded()
	// Remove some keys during rehash
	for i := 0; i < 25; i++ {
		_, ret := d.Remove("k" + strconv.Itoa(i))
		if ret != 1 {
			t.Errorf("expected 1, got %d for key k%d", ret, i)
		}
	}
	if d.Len() != 25 {
		t.Errorf("expected 25, got %d", d.Len())
	}
}

func TestRedisDict_DictScan(t *testing.T) {
	d := MakeRedis(0)
	for i := 0; i < 100; i++ {
		d.Put("key"+strconv.Itoa(i), i)
	}
	for i := 0; i < 100; i++ {
		d.Put("other"+strconv.Itoa(i), i)
	}

	// Scan all with wildcard
	cursor := 0
	result := make([][]byte, 0)
	for {
		keys, next := d.DictScan(cursor, 20, "*")
		result = append(result, keys...)
		if next == 0 {
			break
		}
		cursor = next
	}
	if len(result) != 200 {
		t.Errorf("expected 200, got %d", len(result))
	}

	// Scan with pattern
	cursor = 0
	result = make([][]byte, 0)
	for {
		keys, next := d.DictScan(cursor, 20, "key*")
		result = append(result, keys...)
		if next == 0 {
			break
		}
		cursor = next
	}
	if len(result) != 100 {
		t.Errorf("expected 100, got %d", len(result))
	}
}
