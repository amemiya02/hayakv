package object

import (
	"sort"
)

// Intset is a compact set of integers stored in a sorted slice
type Intset struct {
	data  []int64
	width int // 16, 32, or 64 bits
}

// NewIntset creates a new empty intset
func NewIntset() *Intset {
	return &Intset{
		data:  make([]int64, 0),
		width: 16,
	}
}

// Len returns the number of elements in the intset
func (is *Intset) Len() int {
	return len(is.data)
}

// Width returns the current width (16, 32, or 64)
func (is *Intset) Width() int {
	return is.width
}

// Contains checks if the intset contains the given value
func (is *Intset) Contains(v int64) bool {
	if is == nil || len(is.data) == 0 {
		return false
	}
	idx := sort.Search(len(is.data), func(i int) bool {
		return is.data[i] >= v
	})
	return idx < len(is.data) && is.data[idx] == v
}

// Add adds a value to the intset
// Returns true if the value was added (not already present)
func (is *Intset) Add(v int64) bool {
	if is == nil {
		return false
	}

	// Find insertion point
	idx := sort.Search(len(is.data), func(i int) bool {
		return is.data[i] >= v
	})

	// Already exists
	if idx < len(is.data) && is.data[idx] == v {
		return false
	}

	// Update width if needed
	is.updateWidth(v)

	// Insert at position
	is.data = append(is.data, 0)
	copy(is.data[idx+1:], is.data[idx:])
	is.data[idx] = v
	return true
}

// Remove removes a value from the intset
// Returns true if the value was removed
func (is *Intset) Remove(v int64) bool {
	if is == nil || len(is.data) == 0 {
		return false
	}

	idx := sort.Search(len(is.data), func(i int) bool {
		return is.data[i] >= v
	})

	if idx >= len(is.data) || is.data[idx] != v {
		return false
	}

	// Remove element
	is.data = append(is.data[:idx], is.data[idx+1:]...)
	return true
}

// Range iterates over all elements in sorted order
// If fn returns false, iteration stops
func (is *Intset) Range(fn func(v int64) bool) {
	if is == nil {
		return
	}
	for _, v := range is.data {
		if !fn(v) {
			break
		}
	}
}

// updateWidth updates the width based on the value
func (is *Intset) updateWidth(v int64) {
	if v < -2147483648 || v > 2147483647 {
		is.width = 64
	} else if v < -32768 || v > 32767 {
		if is.width < 32 {
			is.width = 32
		}
	} else {
		if is.width < 16 {
			is.width = 16
		}
	}
}

// ToSlice returns all elements as a slice
func (is *Intset) ToSlice() []int64 {
	if is == nil {
		return nil
	}
	result := make([]int64, len(is.data))
	copy(result, is.data)
	return result
}
