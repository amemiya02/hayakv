package acl

// CategoryCommands maps ACL category names to their command sets.
// Populated by InitCategories which walks the command table.
var CategoryCommands = map[string][]string{}

// KnownCategories lists all recognized ACL categories.
var KnownCategories = []string{
	"read", "write", "admin", "dangerous", "fast", "slow",
	"string", "list", "set", "sortedset", "hash", "stream",
	"bitmap", "hyperloglog", "geo", "pubsub", "scripting",
	"connection", "transaction", "blocking", "generic",
}
