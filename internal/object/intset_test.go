package object

import (
	"testing"
)

func TestIntsetNew(t *testing.T) {
	is := NewIntset()
	if is.Len() != 0 {
		t.Fatalf("new intset should have length 0, got %d", is.Len())
	}
	if is.Width() != 16 {
		t.Fatalf("new intset should have width 16, got %d", is.Width())
	}
}

func TestIntsetAdd(t *testing.T) {
	is := NewIntset()

	// Add elements
	if !is.Add(42) {
		t.Fatal("expected true for new element")
	}
	if !is.Add(10) {
		t.Fatal("expected true for new element")
	}
	if !is.Add(100) {
		t.Fatal("expected true for new element")
	}

	// Verify length
	if is.Len() != 3 {
		t.Fatalf("expected length 3, got %d", is.Len())
	}

	// Add duplicate
	if is.Add(42) {
		t.Fatal("expected false for duplicate element")
	}

	// Verify length unchanged
	if is.Len() != 3 {
		t.Fatalf("expected length 3, got %d", is.Len())
	}
}

func TestIntsetContains(t *testing.T) {
	is := NewIntset()
	is.Add(42)
	is.Add(10)
	is.Add(100)

	// Contains existing elements
	if !is.Contains(42) {
		t.Fatal("expected to contain 42")
	}
	if !is.Contains(10) {
		t.Fatal("expected to contain 10")
	}
	if !is.Contains(100) {
		t.Fatal("expected to contain 100")
	}

	// Does not contain non-existing elements
	if is.Contains(5) {
		t.Fatal("should not contain 5")
	}
	if is.Contains(200) {
		t.Fatal("should not contain 200")
	}
}

func TestIntsetRemove(t *testing.T) {
	is := NewIntset()
	is.Add(42)
	is.Add(10)
	is.Add(100)

	// Remove existing element
	if !is.Remove(42) {
		t.Fatal("expected true for existing element")
	}

	// Verify removed
	if is.Contains(42) {
		t.Fatal("should not contain 42 after removal")
	}
	if is.Len() != 2 {
		t.Fatalf("expected length 2, got %d", is.Len())
	}

	// Remove non-existing element
	if is.Remove(5) {
		t.Fatal("expected false for non-existing element")
	}

	// Verify length unchanged
	if is.Len() != 2 {
		t.Fatalf("expected length 2, got %d", is.Len())
	}
}

func TestIntsetRange(t *testing.T) {
	is := NewIntset()
	is.Add(3)
	is.Add(1)
	is.Add(2)

	// Collect elements in order
	var elements []int64
	is.Range(func(v int64) bool {
		elements = append(elements, v)
		return true
	})

	// Should be sorted
	if len(elements) != 3 {
		t.Fatalf("expected 3 elements, got %d", len(elements))
	}
	if elements[0] != 1 || elements[1] != 2 || elements[2] != 3 {
		t.Fatalf("expected [1, 2, 3], got %v", elements)
	}
}

func TestIntsetRangeEarlyStop(t *testing.T) {
	is := NewIntset()
	is.Add(1)
	is.Add(2)
	is.Add(3)

	var elements []int64
	is.Range(func(v int64) bool {
		elements = append(elements, v)
		return v < 2 // Stop after 2
	})

	if len(elements) != 2 {
		t.Fatalf("expected 2 elements, got %d", len(elements))
	}
	if elements[0] != 1 || elements[1] != 2 {
		t.Fatalf("expected [1, 2], got %v", elements)
	}
}

func TestIntsetWidth(t *testing.T) {
	is := NewIntset()

	// Start with 16-bit width
	if is.Width() != 16 {
		t.Fatalf("expected width 16, got %d", is.Width())
	}

	// Add large value (width increases to 64)
	is.Add(10000000000)
	if is.Width() != 64 {
		t.Fatalf("expected width 64, got %d", is.Width())
	}

	// Add medium value (width stays 64, doesn't shrink)
	is.Add(100000)
	if is.Width() != 64 {
		t.Fatalf("expected width 64, got %d", is.Width())
	}

	// Add small value (width stays 64, doesn't shrink)
	is.Add(42)
	if is.Width() != 64 {
		t.Fatalf("expected width 64, got %d", is.Width())
	}
}

func TestIntsetWidthFromSmall(t *testing.T) {
	is := NewIntset()

	// Add small value first
	is.Add(42)
	if is.Width() != 16 {
		t.Fatalf("expected width 16, got %d", is.Width())
	}

	// Add medium value
	is.Add(100000)
	if is.Width() != 32 {
		t.Fatalf("expected width 32, got %d", is.Width())
	}

	// Add large value
	is.Add(10000000000)
	if is.Width() != 64 {
		t.Fatalf("expected width 64, got %d", is.Width())
	}
}

func TestIntsetWidthNegative(t *testing.T) {
	is := NewIntset()

	// Add negative small value
	is.Add(-42)
	if is.Width() != 16 {
		t.Fatalf("expected width 16, got %d", is.Width())
	}

	// Add negative medium value
	is.Add(-100000)
	if is.Width() != 32 {
		t.Fatalf("expected width 32, got %d", is.Width())
	}

	// Add negative large value
	is.Add(-10000000000)
	if is.Width() != 64 {
		t.Fatalf("expected width 64, got %d", is.Width())
	}
}

func TestIntsetSorted(t *testing.T) {
	is := NewIntset()

	// Add elements in random order
	is.Add(50)
	is.Add(10)
	is.Add(30)
	is.Add(20)
	is.Add(40)

	// Verify sorted order
	expected := []int64{10, 20, 30, 40, 50}
	for i, v := range expected {
		if is.data[i] != v {
			t.Fatalf("expected %d at index %d, got %d", v, i, is.data[i])
		}
	}
}

func TestIntsetToSlice(t *testing.T) {
	is := NewIntset()
	is.Add(3)
	is.Add(1)
	is.Add(2)

	slice := is.ToSlice()
	if len(slice) != 3 {
		t.Fatalf("expected 3 elements, got %d", len(slice))
	}
	if slice[0] != 1 || slice[1] != 2 || slice[2] != 3 {
		t.Fatalf("expected [1, 2, 3], got %v", slice)
	}

	// Verify it's a copy
	slice[0] = 999
	if is.data[0] == 999 {
		t.Fatal("ToSlice should return a copy")
	}
}

func TestIntsetLarge(t *testing.T) {
	is := NewIntset()

	// Add many elements
	for i := 0; i < 1000; i++ {
		is.Add(int64(i))
	}

	if is.Len() != 1000 {
		t.Fatalf("expected length 1000, got %d", is.Len())
	}

	// Verify all elements
	for i := 0; i < 1000; i++ {
		if !is.Contains(int64(i)) {
			t.Fatalf("expected to contain %d", i)
		}
	}
}

func TestIntsetNegative(t *testing.T) {
	is := NewIntset()

	// Add negative numbers
	is.Add(-1)
	is.Add(-10)
	is.Add(-100)

	if is.Len() != 3 {
		t.Fatalf("expected length 3, got %d", is.Len())
	}

	// Verify sorted order (most negative first)
	expected := []int64{-100, -10, -1}
	for i, v := range expected {
		if is.data[i] != v {
			t.Fatalf("expected %d at index %d, got %d", v, i, is.data[i])
		}
	}
}

func TestIntsetMixed(t *testing.T) {
	is := NewIntset()

	// Mix of positive and negative
	is.Add(5)
	is.Add(-5)
	is.Add(0)
	is.Add(10)
	is.Add(-10)

	if is.Len() != 5 {
		t.Fatalf("expected length 5, got %d", is.Len())
	}

	// Verify sorted order
	expected := []int64{-10, -5, 0, 5, 10}
	for i, v := range expected {
		if is.data[i] != v {
			t.Fatalf("expected %d at index %d, got %d", v, i, is.data[i])
		}
	}
}
