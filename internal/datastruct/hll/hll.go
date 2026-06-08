package hll

import "math"

const (
	hllP         = 14
	hllQ         = 50
	hllRegisters = 1 << hllP // 16384
	hllBits      = 6
	hllDenseSize = (hllRegisters*hllBits + 7) / 8 // 12288
	hllSparse    = 0
	hllDense     = 1
	hllHdrSize   = 16
	hashSeed     = 0xadc83b19

	// Redis-compatible sparse opcodes.
	hllSparseMaxZero  = 64    // ZERO: 6-bit count, max 64 registers
	hllSparseMaxVal   = 32    // VAL: 5-bit value (1..32), max 32
	hllSparseMaxXZero = 16384 // XZERO: 14-bit count, max 16384 registers
)

// HLL is a HyperLogLog probabilistic cardinality estimator.
// It wraps a byte slice in the Redis on-wire format.
type HLL struct {
	raw []byte
}

// New creates a new sparse HyperLogLog.
func New() *HLL {
	raw := make([]byte, hllHdrSize+hllSparseMaxXZero)
	copy(raw[:4], "HYLL")
	raw[4] = hllSparse
	// Initialize with a single XZERO covering all registers.
	// XZERO: 01xxxxxx xxxxxxxx, count = registers - 1 = 16383.
	total := hllRegisters - 1
	raw[hllHdrSize] = byte(0x40 | (total >> 8)) // 01xxxxxx
	raw[hllHdrSize+1] = byte(total & 0xFF)      // xxxxxxxx
	return &HLL{raw: raw}
}

// FromBytes wraps an existing byte slice as an HLL, returning nil and false
// if the header is invalid.
func FromBytes(b []byte) (*HLL, bool) {
	if len(b) < hllHdrSize || string(b[:4]) != "HYLL" {
		return nil, false
	}
	return &HLL{raw: b}, true
}

// Bytes returns the raw HLL byte slice.
func (h *HLL) Bytes() []byte {
	return h.raw
}

// ---- Dense helpers ----

func denseGetRegister(raw []byte, reg int) uint8 {
	// Each register is 6 bits, packed into raw[hllHdrSize:] onwards.
	off := hllHdrSize + reg*hllBits
	b0 := raw[off/8]
	b1 := raw[off/8+1]
	return uint8(((uint32(b0) | uint32(b1)<<8) >> (off % 8)) & 0x3F)
}

func denseSetRegister(raw []byte, reg int, val uint8) {
	off := hllHdrSize + reg*hllBits
	idx := off / 8
	bit := off % 8
	// Clear the 6 bits.
	var mask uint16 = 0x3F << bit
	raw[idx] &^= byte(mask)
	raw[idx+1] &^= byte(mask >> 8)
	// Set new value.
	v := uint16(val) << bit
	raw[idx] |= byte(v)
	raw[idx+1] |= byte(v >> 8)
}

// ---- Sparse helpers ----

// sparseDecode decodes a sparse opcode at position i in body.
// Returns opcode (0=ZERO, 1=XZERO, 2=VAL), the argument, and byte advance.
func sparseDecode(body []byte, i int) (opcode int, arg int, advance int) {
	b0 := body[i]
	switch {
	case b0&0xC0 == 0x00: // ZERO: 00xxxxxx
		return 0, int(b0 & 0x3F), 1
	case b0&0xC0 == 0x40: // XZERO: 01xxxxxx xxxxxxxx
		return 1, (int(b0&0x3F))<<8 | int(body[i+1]), 2
	default: // VAL: 1vvvvvxx
		return 2, int(b0>>2) & 0x1F, 1
	}
}

// sparseEncodeZero encodes a ZERO opcode. count is 0..63, representing 1..64 zero registers.
func sparseEncodeZero(count int) []byte {
	return []byte{byte(count & 0x3F)} // 00xxxxxx
}

// sparseEncodeXZero encodes an XZERO opcode. count is 0..16383, representing 1..16384 zero registers.
func sparseEncodeXZero(count int) []byte {
	return []byte{byte(0x40 | (count >> 8)), byte(count & 0xFF)} // 01xxxxxx xxxxxxxx
}

// sparseEncodeVal encodes a VAL opcode. val is 1..32, exactly 1 register.
func sparseEncodeVal(val uint8) []byte {
	return []byte{byte(0x80 | (val << 2))} // 1vvvvvxx
}

// ---- Sparse promotion ----

// promoteToDense converts a sparse HLL to dense encoding in-place.
func (h *HLL) promoteToDense() {
	newRaw := make([]byte, hllHdrSize+hllDenseSize)
	copy(newRaw[:hllHdrSize], h.raw[:hllHdrSize])
	newRaw[4] = hllDense

	// Walk sparse body and copy registers.
	body := h.raw[hllHdrSize:]
	idx := 0
	reg := 0
	for idx < len(body) && reg < hllRegisters {
		op, arg, adv := sparseDecode(body, idx)
		switch op {
		case 0: // ZERO
			reg += arg + 1
		case 1: // XZERO
			reg += arg + 1
		case 2: // VAL
			denseSetRegister(newRaw, reg, uint8(arg+1))
			reg++
		}
		idx += adv
	}

	h.raw = newRaw
}

// ---- Add ----

// hllPatLen counts trailing zeros in hash (after index bits removed) with a
// guard bit set at position HLL_Q (50). Returns the 1-based rank.
func hllPatLen(hash uint64) uint8 {
	// hash already has index bits removed (hash >>= hllP)
	// Set guard bit at position 50 (HLL_Q)
	hash |= uint64(1) << hllQ
	// Count trailing zeros
	var count uint8
	for count < hllQ && hash&(1<<count) == 0 {
		count++
	}
	return count + 1 // 1-based
}

// Add inserts an element into the HyperLogLog. Returns true if the
// estimated cardinality changed.
func (h *HLL) Add(elem []byte) bool {
	hash := murmurHash64A(elem, hashSeed)
	idx := hash & (hllRegisters - 1) // lower 14 bits
	hash >>= hllP                    // remaining 50 bits

	rank := hllPatLen(hash)

	// Get current register value.
	var old uint8
	if h.raw[4] == hllDense {
		old = denseGetRegister(h.raw, int(idx))
	} else {
		old = h.sparseGetRegister(int(idx))
	}

	if rank <= old {
		return false
	}

	// Invalidate cache.
	h.raw[14] = 0

	if h.raw[4] == hllDense {
		denseSetRegister(h.raw, int(idx), rank)
		return true
	}

	// Sparse path: check if we need to promote.
	if !h.sparseSetRegister(int(idx), rank) {
		h.promoteToDense()
		denseSetRegister(h.raw, int(idx), rank)
	}
	return true
}

// ---- Sparse register access ----

// sparseGetRegister returns the value of register target in the sparse encoding.
func (h *HLL) sparseGetRegister(target int) uint8 {
	body := h.raw[hllHdrSize:]
	idx := 0
	reg := 0
	for idx < len(body) {
		op, arg, adv := sparseDecode(body, idx)
		switch op {
		case 0: // ZERO
			if reg+arg+1 > target {
				return 0
			}
			reg += arg + 1
		case 1: // XZERO
			if reg+arg+1 > target {
				return 0
			}
			reg += arg + 1
		case 2: // VAL
			if reg == target {
				return uint8(arg + 1)
			}
			reg++
		}
		idx += adv
	}
	return 0
}

// sparseSetRegister tries to set register target to val in the sparse encoding.
// Returns false if the sparse body would exceed the maximum size, signaling
// that promotion to dense is needed.
func (h *HLL) sparseSetRegister(target int, val uint8) bool {
	body := h.raw[hllHdrSize:]

	// Walk the body and rebuild, replacing the target register with val.
	var newBody []byte

	r := 0
	i := 0
	for i < len(body) {
		op, arg, adv := sparseDecode(body, i)
		var count int
		switch op {
		case 0: // ZERO
			count = arg + 1
		case 1: // XZERO
			count = arg + 1
		case 2: // VAL
			count = 1
		}

		if r+count <= target || r > target {
			// Opcode is entirely before or after the target — copy as-is.
			newBody = append(newBody, body[i:i+adv]...)
		} else {
			// This opcode contains the target register. Split it.
			before := target - r
			after := r + count - target - 1

			if op == 2 {
				// VAL — exactly 1 register, replace value.
				newBody = append(newBody, sparseEncodeVal(val)...)
			} else {
				// ZERO or XZERO — split into before, val, after.
				if before > 0 {
					if op == 1 {
						newBody = append(newBody, sparseEncodeXZero(before-1)...)
					} else {
						newBody = append(newBody, sparseEncodeZero(before-1)...)
					}
				}
				newBody = append(newBody, sparseEncodeVal(val)...)
				if after > 0 {
					// remaining zeros: use ZERO or XZERO depending on size
					if after <= hllSparseMaxZero {
						newBody = append(newBody, sparseEncodeZero(after-1)...)
					} else {
						newBody = append(newBody, sparseEncodeXZero(after-1)...)
					}
				}
			}
		}

		r += count
		i += adv
	}

	// Check if it fits.
	if hllHdrSize+len(newBody) > hllHdrSize+hllDenseSize {
		return false // promote to dense
	}

	// Coalesce adjacent opcodes to minimize size.
	newBody = coalesceSparse(newBody)

	if hllHdrSize+len(newBody) > hllHdrSize+hllDenseSize {
		return false
	}

	// Replace the body.
	h.raw = append(h.raw[:hllHdrSize], newBody...)
	return true
}

// coalesceSparse merges adjacent opcodes of the same type to minimize size.
func coalesceSparse(body []byte) []byte {
	// Decode all opcodes into (type, count/value) pairs.
	type sop struct {
		kind  int // 0=ZERO, 1=XZERO, 2=VAL
		count int // for ZERO/XZERO: number of zero registers; for VAL: value
	}
	var ops []sop
	i := 0
	for i < len(body) {
		op, arg, adv := sparseDecode(body, i)
		switch op {
		case 0: // ZERO
			ops = append(ops, sop{kind: 0, count: arg + 1})
		case 1: // XZERO
			ops = append(ops, sop{kind: 1, count: arg + 1})
		case 2: // VAL
			ops = append(ops, sop{kind: 2, count: arg + 1})
		}
		i += adv
	}

	// Merge adjacent ZERO and XZERO runs.
	var merged []sop
	for _, o := range ops {
		if len(merged) > 0 {
			last := &merged[len(merged)-1]
			if last.kind == 0 && o.kind == 0 {
				last.count += o.count
				continue
			}
			if last.kind == 0 && o.kind == 1 {
				last.count += o.count
				last.kind = 1
				continue
			}
			if last.kind == 1 && o.kind == 0 {
				last.count += o.count
				continue
			}
			if last.kind == 1 && o.kind == 1 {
				last.count += o.count
				continue
			}
		}
		merged = append(merged, o)
	}

	// Re-serialize, splitting large runs.
	var out []byte
	for _, o := range merged {
		switch o.kind {
		case 0: // ZERO
			rem := o.count
			for rem > 0 {
				n := rem
				if n > hllSparseMaxZero {
					n = hllSparseMaxZero
				}
				out = append(out, sparseEncodeZero(n-1)...)
				rem -= n
			}
		case 1: // XZERO
			rem := o.count
			for rem > 0 {
				n := rem
				if n > hllSparseMaxXZero {
					n = hllSparseMaxXZero
				}
				out = append(out, sparseEncodeXZero(n-1)...)
				rem -= n
			}
		case 2: // VAL
			out = append(out, sparseEncodeVal(uint8(o.count))...)
		}
	}
	return out
}

// ---- Count (estimate) ----

// hllSigma computes sigma(x) for the HyperLogLog bias correction.
func hllSigma(x float64) float64 {
	if x == 1.0 {
		return math.Inf(1)
	}
	z := x
	y := 1.0
	for {
		x *= x
		zp := z
		z += x * y
		y += y
		if zp == z {
			break
		}
	}
	return z
}

// hllTau computes tau(x) for the HyperLogLog bias correction.
func hllTau(x float64) float64 {
	if x == 0.0 || x == 1.0 {
		return 0.0
	}
	y := 1.0
	z := 1 - x
	for {
		x = math.Sqrt(x)
		zp := z
		y *= 0.5
		z -= math.Pow(1-x, 2) * y
		if zp == z {
			break
		}
	}
	return z / 3.0
}

// Count returns the estimated cardinality of the HyperLogLog.
// Uses the Ertl 2017 estimator matching Redis 8's hllCount.
func (h *HLL) Count() uint64 {
	// Check cache validity.
	if h.raw[14] != 0 {
		return uint64(h.raw[5]) |
			uint64(h.raw[6])<<8 |
			uint64(h.raw[7])<<16 |
			uint64(h.raw[8])<<24 |
			uint64(h.raw[9])<<32 |
			uint64(h.raw[10])<<40 |
			uint64(h.raw[11])<<48 |
			uint64(h.raw[12])<<56
	}

	// Collect register values.
	registers := make([]uint8, hllRegisters)
	if h.raw[4] == hllDense {
		for i := 0; i < hllRegisters; i++ {
			registers[i] = denseGetRegister(h.raw, i)
		}
	} else {
		// Walk sparse body
		body := h.raw[hllHdrSize:]
		idx := 0
		reg := 0
		for idx < len(body) && reg < hllRegisters {
			op, arg, adv := sparseDecode(body, idx)
			switch op {
			case 0: // ZERO
				reg += arg + 1
			case 1: // XZERO
				reg += arg + 1
			case 2: // VAL
				registers[reg] = uint8(arg + 1)
				reg++
			}
			idx += adv
		}
	}

	// Ertl 2017 estimator (matching Redis hllCount exactly).
	// Build register histogram: reghisto[i] = count of registers with value i.
	m := float64(hllRegisters)
	var reghisto [52]int // values 0..51
	for _, v := range registers {
		reghisto[v]++
	}

	// z = m * tau((m - reghisto[51]) / m)
	z := m * hllTau((m-float64(reghisto[51]))/m)

	// Fold: for j = 50 down to 1: z += reghisto[j]; z *= 0.5
	for j := 50; j >= 1; j-- {
		z += float64(reghisto[j])
		z *= 0.5
	}

	// z += m * sigma(reghisto[0] / m)
	z += m * hllSigma(float64(reghisto[0])/m)

	// E = alpha_m * m^2 / z, where alpha_m = 1/(2*ln(2)) ≈ 0.72134752
	E := 0.72134752 * m * m / z

	est := uint64(E + 0.5)

	// Update cache.
	h.raw[5] = byte(est)
	h.raw[6] = byte(est >> 8)
	h.raw[7] = byte(est >> 16)
	h.raw[8] = byte(est >> 24)
	h.raw[9] = byte(est >> 32)
	h.raw[10] = byte(est >> 40)
	h.raw[11] = byte(est >> 48)
	h.raw[12] = byte(est >> 56)
	h.raw[14] = 1

	return est
}

// ---- Merge ----

// Merge combines other into h by taking the register-wise maximum.
func (h *HLL) Merge(other *HLL) {
	for i := 0; i < hllRegisters; i++ {
		var aVal, bVal uint8
		if h.raw[4] == hllDense {
			aVal = denseGetRegister(h.raw, i)
		} else {
			aVal = h.sparseGetRegister(i)
		}
		if other.raw[4] == hllDense {
			bVal = denseGetRegister(other.raw, i)
		} else {
			bVal = other.sparseGetRegister(i)
		}
		if bVal > aVal {
			if h.raw[4] != hllDense {
				if !h.sparseSetRegister(i, bVal) {
					h.promoteToDense()
					denseSetRegister(h.raw, i, bVal)
				}
			} else {
				denseSetRegister(h.raw, i, bVal)
			}
		}
	}
	// Invalidate cache.
	h.raw[14] = 0
}
