package hll

import "math"

const (
	hllP         = 14
	hllRegisters = 1 << hllP // 16384
	hllBits      = 6
	hllDenseSize = (hllRegisters*hllBits + 7) / 8 // 12288
	hllSparse    = 0
	hllDense     = 1
	hllHdrSize   = 16
	hashSeed     = 0xadc83b19

	// Sparse opcodes.
	hllSparseZeroBit  = 0x80 // 10000000
	hllSparseValBit   = 0x80 // 1xxxxxxx (if bit 7 set and not XZERO)
	hllSparseXZeroBit = 0xC0 // 11xxxxxx for XZERO

	// Maximum run lengths.
	hllSparseMaxZero  = 64    // ZERO: 6-bit count + 1, max 64
	hllSparseMaxVal   = 64    // VAL: 6-bit value + 1, max 64 (value 1..64)
	hllSparseMaxXZero = 16384 // XZERO: 14-bit count + 1
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

	// Initialize sparse body: a single XZERO covering all registers.
	// XZERO opcode: 11xxxxxx xxxxxxxx, count = registers - 1 = 16383.
	total := hllRegisters - 1 // 16383
	raw[hllHdrSize] = byte(hllSparseXZeroBit | (total >> 8))
	raw[hllHdrSize+1] = byte(total & 0xFF)
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
		b0 := body[idx]
		if b0&hllSparseXZeroBit == hllSparseXZeroBit && b0 != hllSparseXZeroBit {
			// XZERO: 11xxxxxx xxxxxxxx
			if idx+1 >= len(body) {
				break
			}
			count := int(b0&0x3F)<<8 | int(body[idx+1])
			count++
			reg += count
			idx += 2
		} else if b0&0x80 != 0 {
			// VAL: 1xxxxxxx, value = (b0 & 0x7F) + 1
			val := uint8(b0&0x7F) + 1
			denseSetRegister(newRaw, reg, val)
			reg++
			idx++
		} else {
			// ZERO: 0xxxxxxx, count = (b0 & 0x7F) + 1
			count := int(b0&0x7F) + 1
			reg += count
			idx++
		}
	}

	h.raw = newRaw
}

// ---- Add ----

// Add inserts an element into the HyperLogLog. Returns true if the
// estimated cardinality changed.
func (h *HLL) Add(elem []byte) bool {
	hash := murmurHash64A(elem, hashSeed)
	idx := hash & (hllRegisters - 1) // lower 14 bits
	hash >>= hllP                    // remaining 50 bits in positions 49..0

	// Count leading zeros in the 50-bit value (positions 49..0).
	// In a 64-bit word, the 50-bit value has 14 leading zeros at most (bits 63..50).
	// rank = clz(hash) - 14 + 1, capped at 50 (fits in 6 bits: max 63).
	var count uint8
	if hash != 0 {
		// Count leading zeros manually for the 50-bit value.
		for i := 63; i >= 0; i-- {
			if hash&(1<<uint(i)) != 0 {
				break
			}
			count++
		}
		count = count - 14 + 1 // subtract the 14 zeros from the 64-bit word, +1 for 1-based
	} else {
		count = 50 // all 50 bits are zero
	}
	if count > 63 {
		count = 63 // cap at max for 6-bit register
	}

	// Get current register value.
	var old uint8
	if h.raw[4] == hllDense {
		old = denseGetRegister(h.raw, int(idx))
	} else {
		old = h.sparseGetRegister(int(idx))
	}

	if count <= old {
		return false
	}

	// Invalidate cache.
	h.raw[14] = 0

	if h.raw[4] == hllDense {
		denseSetRegister(h.raw, int(idx), count)
		return true
	}

	// Sparse path: check if we need to promote.
	if !h.sparseSetRegister(int(idx), count) {
		h.promoteToDense()
		denseSetRegister(h.raw, int(idx), count)
	}
	return true
}

// ---- Sparse register access ----

// sparseGetRegister returns the value of register idx in the sparse encoding.
func (h *HLL) sparseGetRegister(target int) uint8 {
	body := h.raw[hllHdrSize:]
	idx := 0
	reg := 0
	for idx < len(body) {
		b0 := body[idx]
		if b0&hllSparseXZeroBit == hllSparseXZeroBit && b0 != hllSparseXZeroBit {
			// XZERO
			if idx+1 >= len(body) {
				break
			}
			count := int(b0&0x3F)<<8 | int(body[idx+1])
			count++
			if reg+count > target {
				return 0
			}
			reg += count
			idx += 2
		} else if b0&0x80 != 0 {
			// VAL
			if reg == target {
				return uint8(b0&0x7F) + 1
			}
			reg++
			idx++
		} else {
			// ZERO
			count := int(b0&0x7F) + 1
			if reg+count > target {
				return 0
			}
			reg += count
			idx++
		}
	}
	return 0
}

// sparseSetRegister tries to set register target to val in the sparse encoding.
// Returns false if the sparse body would exceed the maximum size, signaling
// that promotion to dense is needed.
func (h *HLL) sparseSetRegister(target int, val uint8) bool {
	body := h.raw[hllHdrSize:]
	// Walk to find the opcode containing the target register.
	reg := 0
	opIdx := 0
	for opIdx < len(body) {
		b0 := body[opIdx]
		var count int
		if b0&hllSparseXZeroBit == hllSparseXZeroBit && b0 != hllSparseXZeroBit {
			// XZERO
			count = int(b0&0x3F)<<8 | int(body[opIdx+1])
			count++
		} else if b0&0x80 != 0 {
			// VAL
			count = 1
		} else {
			// ZERO
			count = int(b0&0x7F) + 1
		}
		if reg+count > target {
			break
		}
		reg += count
		if b0&hllSparseXZeroBit == hllSparseXZeroBit && b0 != hllSparseXZeroBit {
			opIdx += 2
		} else {
			opIdx++
		}
	}

	// We found the opcode at opIdx containing register `target`.
	// We need to split/reconstruct the sparse body.
	// The approach: rebuild the sparse body, replacing the relevant opcode
	// with the new sequence.

	// Count the opcodes and figure out how many bytes the replacement needs.
	// Strategy: serialize everything into a temp buffer, replacing the target.

	var newBody []byte
	writeXZero := func(count int) {
		c := count - 1
		newBody = append(newBody, byte(hllSparseXZeroBit|(c>>8)), byte(c&0xFF))
	}
	writeZero := func(count int) {
		c := count - 1
		newBody = append(newBody, byte(c&0x7F))
	}
	writeVal := func(v uint8) {
		newBody = append(newBody, byte(hllSparseValBit|(v-1)))
	}

	// Walk again and rebuild.
	r := 0
	i := 0
	for i < len(body) {
		b0 := body[i]
		var count int
		var isXZero bool
		if b0&hllSparseXZeroBit == hllSparseXZeroBit && b0 != hllSparseXZeroBit {
			count = int(b0&0x3F)<<8 | int(body[i+1])
			count++
			isXZero = true
		} else if b0&0x80 != 0 {
			count = 1
		} else {
			count = int(b0&0x7F) + 1
		}

		if r+count <= target || r > target {
			// Opcode is entirely before or after the target — copy as-is.
			if isXZero {
				newBody = append(newBody, body[i], body[i+1])
			} else {
				newBody = append(newBody, body[i])
			}
		} else {
			// This opcode contains the target register. We need to split it.
			before := target - r            // registers before target in this opcode
			after := r + count - target - 1 // registers after target in this opcode

			if b0&0x80 != 0 && !isXZero {
				// VAL opcode — it's exactly 1 register. Replace with new value.
				writeVal(val)
			} else {
				// ZERO or XZERO opcode — split into before, val, after.
				if before > 0 {
					if isXZero {
						writeXZero(before)
					} else {
						writeZero(before)
					}
				}
				writeVal(val)
				if after > 0 {
					writeZero(after) // new registers are zero-valued
				}
			}
		}

		r += count
		if isXZero {
			i += 2
		} else {
			i++
		}
	}

	// Now newBody is the rebuilt sparse body. Check if it fits.
	if hllHdrSize+len(newBody) > hllHdrSize+hllDenseSize {
		return false // promote to dense
	}

	// Coalesce adjacent ZERO opcodes and merge ZERO with XZERO where possible
	// for a compact representation. For simplicity and correctness, we do a
	// final pass that merges adjacent runs of the same type.
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
	type op struct {
		kind  int // 0=ZERO, 1=VAL, 2=XZERO
		count int // for ZERO/XZERO: number of zero registers; for VAL: value
	}
	var ops []op
	i := 0
	for i < len(body) {
		b0 := body[i]
		if b0&hllSparseXZeroBit == hllSparseXZeroBit && b0 != hllSparseXZeroBit {
			c := int(b0&0x3F)<<8 | int(body[i+1])
			ops = append(ops, op{kind: 2, count: c + 1})
			i += 2
		} else if b0&0x80 != 0 {
			ops = append(ops, op{kind: 1, count: int(b0&0x7F) + 1})
			i++
		} else {
			ops = append(ops, op{kind: 0, count: int(b0&0x7F) + 1})
			i++
		}
	}

	// Merge adjacent ZERO and XZERO runs.
	var merged []op
	for _, o := range ops {
		if len(merged) > 0 {
			last := &merged[len(merged)-1]
			if last.kind == 0 && o.kind == 0 {
				last.count += o.count
				continue
			}
			if last.kind == 0 && o.kind == 2 {
				last.count += o.count
				last.kind = 2
				continue
			}
			if last.kind == 2 && o.kind == 0 {
				last.count += o.count
				continue
			}
			if last.kind == 2 && o.kind == 2 {
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
				out = append(out, byte((n-1)&0x7F))
				rem -= n
			}
		case 1: // VAL
			out = append(out, byte(hllSparseValBit|(o.count-1)))
		case 2: // XZERO
			rem := o.count
			for rem > 0 {
				n := rem
				if n > hllSparseMaxXZero {
					n = hllSparseMaxXZero
				}
				c := n - 1
				out = append(out, byte(hllSparseXZeroBit|(c>>8)), byte(c&0xFF))
				rem -= n
			}
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
		body := h.raw[hllHdrSize:]
		idx := 0
		reg := 0
		for idx < len(body) && reg < hllRegisters {
			b0 := body[idx]
			if b0&hllSparseXZeroBit == hllSparseXZeroBit && b0 != hllSparseXZeroBit {
				if idx+1 >= len(body) {
					break
				}
				count := int(b0&0x3F)<<8 | int(body[idx+1])
				count++
				reg += count
				idx += 2
			} else if b0&0x80 != 0 {
				registers[reg] = uint8(b0&0x7F) + 1
				reg++
				idx++
			} else {
				count := int(b0&0x7F) + 1
				reg += count
				idx++
			}
		}
	}

	// Count zeros.
	var ez int
	for _, v := range registers {
		if v == 0 {
			ez++
		}
	}

	// Raw HLL estimate: E = alpha * m^2 / sum(2^-Rj)
	// Zero registers contribute 2^0 = 1 to the sum.
	E := hllRawEstimate(registers)

	// For small estimates (E <= 5*m) with empty registers, use linear counting.
	// This is the standard HLL correction that gives accurate results for
	// cardinalities much smaller than the number of registers.
	if E <= 5.0*float64(hllRegisters) && ez != 0 {
		E = float64(hllRegisters) * math.Log(float64(hllRegisters)/float64(ez))
	}

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

// hllRawEstimate computes the raw HyperLogLog estimate from register values.
// This is alpha_m * m^2 / sum(2^-Rj), where alpha_m is the bias correction
// constant and m is the number of registers.
func hllRawEstimate(registers []uint8) float64 {
	var sum float64
	for _, v := range registers {
		sum += 1.0 / math.Pow(2.0, float64(v))
	}
	// alpha_m = 1 / (2 * ln(2)) for the standard HLL estimator.
	alpha := 1.0 / (2.0 * math.Log(2.0))
	m := float64(hllRegisters)
	return alpha * m * m / sum
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
