package dict

import (
	"testing"

	"pgregory.net/rapid"
)

func genKey() *rapid.Generator[string] {
	return rapid.StringOfN(rapid.RuneFrom([]rune("abcdef0123456789")), 1, 20, 20)
}

// TestRedisDictPropertyPutGet verifies that Put followed by Get always returns the last value.
func TestRedisDictPropertyPutGet(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		d := MakeRedis(4)
		keys := rapid.SliceOfN(genKey(), 1, 50).Draw(t, "keys")
		vals := make([]int, len(keys))
		for i := range vals {
			vals[i] = rapid.IntRange(0, 1000).Draw(t, "val")
		}

		// Track expected last value per key
		expected := make(map[string]int)
		for i, k := range keys {
			d.Put(k, vals[i])
			expected[k] = vals[i]
		}

		for k, want := range expected {
			val, ok := d.Get(k)
			if !ok {
				t.Fatalf("key %q not found after Put", k)
			}
			if val != want {
				t.Fatalf("key %q: expected %v, got %v", k, want, val)
			}
		}
	})
}

// TestRedisDictPropertyLen verifies that Len matches the number of distinct keys inserted.
func TestRedisDictPropertyLen(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		d := MakeRedis(4)
		keys := rapid.SliceOfN(genKey(), 1, 30).Draw(t, "keys")

		distinct := make(map[string]struct{})
		for _, k := range keys {
			d.Put(k, 0)
			distinct[k] = struct{}{}
		}

		if d.Len() != len(distinct) {
			t.Fatalf("Len()=%d, want %d", d.Len(), len(distinct))
		}
	})
}

// TestRedisDictPropertyRemove verifies that Remove makes keys inaccessible.
func TestRedisDictPropertyRemove(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		d := MakeRedis(4)
		keys := rapid.SliceOfN(genKey(), 1, 30).Draw(t, "keys")

		for _, k := range keys {
			d.Put(k, 0)
		}

		for _, k := range keys {
			d.Remove(k)
			_, ok := d.Get(k)
			if ok {
				t.Fatalf("key %q still accessible after Remove", k)
			}
		}

		if d.Len() != 0 {
			t.Fatalf("Len()=%d after removing all keys, want 0", d.Len())
		}
	})
}

// TestRedisDictPropertyPutIfAbsent verifies idempotence.
func TestRedisDictPropertyPutIfAbsent(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		d := MakeRedis(4)
		key := genKey().Draw(t, "key")
		v1 := rapid.Int().Draw(t, "v1")
		v2 := rapid.Int().Draw(t, "v2")

		d.PutIfAbsent(key, v1)
		d.PutIfAbsent(key, v2)

		val, ok := d.Get(key)
		if !ok {
			t.Fatalf("key %q not found", key)
		}
		if val != v1 {
			t.Fatalf("PutIfAbsent overwrote: got %v, want %v", val, v1)
		}
	})
}

// TestRedisDictPropertyPutIfExists only updates existing keys.
func TestRedisDictPropertyPutIfExists(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		d := MakeRedis(4)
		key := genKey().Draw(t, "key")
		v1 := rapid.Int().Draw(t, "v1")
		v2 := rapid.Int().Draw(t, "v2")

		// Should not insert
		ret := d.PutIfExists(key, v1)
		if ret != 0 {
			t.Fatalf("PutIfExists on missing key returned %d, want 0", ret)
		}

		d.Put(key, v1)
		ret = d.PutIfExists(key, v2)
		if ret != 1 {
			t.Fatalf("PutIfExists on existing key returned %d, want 1", ret)
		}

		val, _ := d.Get(key)
		if val != v2 {
			t.Fatalf("expected %v, got %v", v2, val)
		}
	})
}

// TestRedisDictPropertyClear verifies that Clear removes all entries.
func TestRedisDictPropertyClear(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		d := MakeRedis(4)
		keys := rapid.SliceOfN(genKey(), 1, 30).Draw(t, "keys")
		for _, k := range keys {
			d.Put(k, 0)
		}
		d.Clear()
		if d.Len() != 0 {
			t.Fatalf("Len()=%d after Clear, want 0", d.Len())
		}
	})
}

// TestRedisDictPropertyKeysMatchesLen verifies Keys() returns exactly Len() items.
func TestRedisDictPropertyKeysMatchesLen(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		d := MakeRedis(4)
		keys := rapid.SliceOfN(genKey(), 1, 30).Draw(t, "keys")
		for _, k := range keys {
			d.Put(k, 0)
		}
		got := d.Keys()
		if len(got) != d.Len() {
			t.Fatalf("Keys() len=%d, Len()=%d", len(got), d.Len())
		}
	})
}
