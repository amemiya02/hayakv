package object

// sharedIntegers is a pool of pre-allocated Robj for small integers (0..9999),
// mirroring Redis's OBJ_SHARED_INTEGERS.
var sharedIntegers [10000]*Robj

func init() {
	for i := 0; i < 10000; i++ {
		sharedIntegers[i] = &Robj{
			Type:     TypeString,
			Encoding: EncInt,
			Ptr:      int64(i),
		}
	}
}

// SharedInt returns a shared Robj for integers in [0, 10000).
// Values outside this range get a fresh allocation.
func SharedInt(n int64) *Robj {
	if n >= 0 && n < 10000 {
		return sharedIntegers[n]
	}
	return &Robj{
		Type:     TypeString,
		Encoding: EncInt,
		Ptr:      n,
	}
}
