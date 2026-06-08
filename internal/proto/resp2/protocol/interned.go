package protocol

// Interned singletons for common replies to reduce allocations.

var theNullBulkReply = &NullBulkReply{}
var theEmptyMultiBulkReply = &EmptyMultiBulkReply{}

// Common IntReply singletons
var intReply0 = &IntReply{Code: 0}
var intReply1 = &IntReply{Code: 1}

// MakeNullBulkReply returns a shared NullBulkReply singleton.
func MakeNullBulkReply() *NullBulkReply {
	return theNullBulkReply
}

// MakeEmptyMultiBulkReply returns a shared EmptyMultiBulkReply singleton.
func MakeEmptyMultiBulkReply() *EmptyMultiBulkReply {
	return theEmptyMultiBulkReply
}

// MakeIntReply creates an IntReply. For common values 0 and 1, returns a singleton.
func MakeIntReply(code int64) *IntReply {
	switch code {
	case 0:
		return intReply0
	case 1:
		return intReply1
	default:
		return &IntReply{Code: code}
	}
}
