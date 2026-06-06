package eventloop

// blockRegistry tracks clients suspended by blocking commands (BLPOP/BRPOP)
// and enables wakeups when LPUSH/RPUSH modifies the target key.
type blockRegistry struct {
	// waiterMap maps a key to the list of clients waiting on it.
	waiterMap map[string][]*client
}

func newBlockRegistry() *blockRegistry {
	return &blockRegistry{
		waiterMap: make(map[string][]*client),
	}
}

// block suspends a client, registering it as a waiter on the given keys.
// The client will be woken when any of those keys is pushed to.
func (r *blockRegistry) block(c *client, keys []string) {
	for _, key := range keys {
		r.waiterMap[key] = append(r.waiterMap[key], c)
	}
}

// waiters returns the list of clients waiting on the given key.
func (r *blockRegistry) waiters(key string) []*client {
	return r.waiterMap[key]
}

// unblock removes a client from all waiter lists.
func (r *blockRegistry) unblock(c *client) {
	for key, list := range r.waiterMap {
		for i, w := range list {
			if w == c {
				r.waiterMap[key] = append(list[:i], list[i+1:]...)
				break
			}
		}
		if len(r.waiterMap[key]) == 0 {
			delete(r.waiterMap, key)
		}
	}
}
