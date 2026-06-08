package object

// SetFieldExpire sets a relative TTL (from now) on a hash field.
// The expiry is stored as an absolute millisecond timestamp.
func (h *Hash) SetFieldExpire(field string, expireAtMs int64) {
	if h.fieldExpire == nil {
		h.fieldExpire = make(map[string]int64)
	}
	h.fieldExpire[field] = expireAtMs
}

// FieldExpire returns the absolute millisecond expiry timestamp for a field,
// and true if the field has an expiry set.
func (h *Hash) FieldExpire(field string) (int64, bool) {
	if h.fieldExpire == nil {
		return 0, false
	}
	exp, ok := h.fieldExpire[field]
	return exp, ok
}

// PersistField removes the expiry from a hash field. Returns true if the
// field had an expiry.
func (h *Hash) PersistField(field string) bool {
	if h.fieldExpire == nil {
		return false
	}
	_, ok := h.fieldExpire[field]
	if ok {
		delete(h.fieldExpire, field)
	}
	return ok
}

// PurgeExpiredFields removes all expired fields and returns their names.
// nowMs is the current time in milliseconds since epoch.
func (h *Hash) PurgeExpiredFields(nowMs int64) []string {
	if h.fieldExpire == nil {
		return nil
	}
	var expired []string
	for field, exp := range h.fieldExpire {
		if exp <= nowMs {
			expired = append(expired, field)
			h.Remove(field) // remove from the actual hash data
			delete(h.fieldExpire, field)
		}
	}
	return expired
}

// HasFieldExpiries returns true if any field has an expiry set.
func (h *Hash) HasFieldExpiries() bool {
	return len(h.fieldExpire) > 0
}

// FieldExpireCount returns the number of fields with expiry set.
func (h *Hash) FieldExpireCount() int {
	return len(h.fieldExpire)
}
