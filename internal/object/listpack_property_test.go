package object

import (
	"fmt"
	"pgregory.net/rapid"
	"testing"
)

func TestListpackPropertyRoundTrip(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		lp := NewListpack()

		// Generate random elements
		numElements := rapid.IntRange(0, 100).Draw(t, "numElements")
		elements := make([]interface{}, numElements)

		for i := 0; i < numElements; i++ {
			if rapid.Bool().Draw(t, "isString") {
				// String element
				s := rapid.StringOf(rapid.RuneFrom([]rune("abcdefghijklmnopqrstuvwxyz0123456789"))).
					Draw(t, fmt.Sprintf("str_%d", i))
				lp.AppendStr(s)
				elements[i] = s
			} else {
				// Int element
				v := rapid.Int64().Draw(t, fmt.Sprintf("int_%d", i))
				lp.AppendInt(v)
				elements[i] = v
			}
		}

		// Verify length
		if lp.Len() != numElements {
			t.Fatalf("expected length %d, got %d", numElements, lp.Len())
		}

		// Verify each element
		for i := 0; i < numElements; i++ {
			val, ok := lp.Get(i)
			if !ok {
				t.Fatalf("failed to get element at index %d", i)
			}

			expected := elements[i]
			switch expectedVal := expected.(type) {
			case string:
				actualVal, isStr := val.(string)
				if !isStr {
					t.Fatalf("expected string at index %d, got %T", i, val)
				}
				if actualVal != expectedVal {
					t.Fatalf("expected %q at index %d, got %q", expectedVal, i, actualVal)
				}
			case int64:
				actualVal, isInt := val.(int64)
				if !isInt {
					t.Fatalf("expected int64 at index %d, got %T", i, val)
				}
				if actualVal != expectedVal {
					t.Fatalf("expected %d at index %d, got %d", expectedVal, i, actualVal)
				}
			default:
				t.Fatalf("unexpected type at index %d: %T", i, expected)
			}
		}

		// Verify Range
		rangeCount := 0
		lp.Range(func(i int, val interface{}) bool {
			rangeCount++
			return true
		})
		if rangeCount != numElements {
			t.Fatalf("Range visited %d elements, expected %d", rangeCount, numElements)
		}
	})
}

func TestListpackPropertyBytesRoundTrip(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		lp := NewListpack()

		// Generate random elements
		numElements := rapid.IntRange(0, 50).Draw(t, "numElements")
		elements := make([]interface{}, numElements)

		for i := 0; i < numElements; i++ {
			if rapid.Bool().Draw(t, "isString") {
				s := rapid.StringOf(rapid.RuneFrom([]rune("abcdefghijklmnopqrstuvwxyz"))).
					Draw(t, fmt.Sprintf("str_%d", i))
				lp.AppendStr(s)
				elements[i] = s
			} else {
				v := rapid.Int64().Draw(t, fmt.Sprintf("int_%d", i))
				lp.AppendInt(v)
				elements[i] = v
			}
		}

		// Get bytes
		bytes := lp.Bytes()
		if len(bytes) == 0 && numElements > 0 {
			t.Fatal("expected non-empty bytes for non-empty listpack")
		}

		// Decode from bytes
		pos := 0
		for i := 0; i < numElements; i++ {
			if pos >= len(bytes) {
				t.Fatalf("unexpected end of bytes at element %d", i)
			}

			val, consumed, _ := decodeElem(bytes[pos:])
			if consumed == 0 {
				t.Fatalf("failed to decode element at index %d", i)
			}
			pos += consumed

			expected := elements[i]
			switch expectedVal := expected.(type) {
			case string:
				actualVal, isStr := val.(string)
				if !isStr {
					t.Fatalf("expected string at index %d, got %T", i, val)
				}
				if actualVal != expectedVal {
					t.Fatalf("expected %q at index %d, got %q", expectedVal, i, actualVal)
				}
			case int64:
				actualVal, isInt := val.(int64)
				if !isInt {
					t.Fatalf("expected int64 at index %d, got %T", i, val)
				}
				if actualVal != expectedVal {
					t.Fatalf("expected %d at index %d, got %d", expectedVal, i, actualVal)
				}
			}
		}
	})
}

func TestListpackPropertyAppendAll(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		lp := NewListpack()

		// Generate string elements
		numStrings := rapid.IntRange(0, 50).Draw(t, "numStrings")
		strs := make([]string, numStrings)
		for i := 0; i < numStrings; i++ {
			s := rapid.StringOf(rapid.RuneFrom([]rune("abc"))).
				Draw(t, fmt.Sprintf("str_%d", i))
			lp.AppendStr(s)
			strs[i] = s
		}

		// Generate int elements
		numInts := rapid.IntRange(0, 50).Draw(t, "numInts")
		ints := make([]int64, numInts)
		for i := 0; i < numInts; i++ {
			v := rapid.Int64().Draw(t, fmt.Sprintf("int_%d", i))
			lp.AppendInt(v)
			ints[i] = v
		}

		// Verify total length
		expectedLen := numStrings + numInts
		if lp.Len() != expectedLen {
			t.Fatalf("expected length %d, got %d", expectedLen, lp.Len())
		}

		// Verify string elements
		for i := 0; i < numStrings; i++ {
			val, ok := lp.Get(i)
			if !ok {
				t.Fatalf("failed to get string element at index %d", i)
			}
			if val.(string) != strs[i] {
				t.Fatalf("expected %q at index %d, got %q", strs[i], i, val)
			}
		}

		// Verify int elements
		for i := 0; i < numInts; i++ {
			val, ok := lp.Get(numStrings + i)
			if !ok {
				t.Fatalf("failed to get int element at index %d", numStrings+i)
			}
			if val.(int64) != ints[i] {
				t.Fatalf("expected %d at index %d, got %d", ints[i], numStrings+i, val)
			}
		}
	})
}

func TestListpackPropertyEmptyGet(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		lp := NewListpack()

		// Get on empty list should return false
		_, ok := lp.Get(0)
		if ok {
			t.Fatal("expected false for Get on empty listpack")
		}

		// Range on empty list should not call fn
		called := false
		lp.Range(func(i int, val interface{}) bool {
			called = true
			return true
		})
		if called {
			t.Fatal("expected Range not to call fn on empty listpack")
		}
	})
}
