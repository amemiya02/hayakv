package acl

import (
	"fmt"
	"sync"
	"time"
)

// LogEntry records an ACL event (auth failure, NOPERM).
type LogEntry struct {
	Timestamp  int64
	Reason     string // "auth" or "command" or "key" or "channel"
	User       string
	ClientAddr string
	Details    string
}

// ACL manages the user registry.
type ACL struct {
	users   map[string]*User
	mu      sync.RWMutex
	log     []LogEntry
	logSize int
}

// NewACL creates the ACL registry with a default user that has all permissions.
func NewACL() *ACL {
	a := &ACL{
		users:   map[string]*User{},
		logSize: 128,
	}
	// Seed the default user: on, nopass, all permissions.
	defaultUser := NewUser("default")
	defaultUser.Enabled = true
	defaultUser.NoPass = true
	defaultUser.AllKeys = true
	defaultUser.AllChans = true
	defaultUser.AllCmds = true
	a.users["default"] = defaultUser
	return a
}

// SetUser creates or updates a user with the given ACL rules.
func (a *ACL) SetUser(name string, rules []string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	u, ok := a.users[name]
	if !ok {
		u = NewUser(name)
		a.users[name] = u
	}
	return u.ApplyRules(rules)
}

// GetUser returns the named user and whether it exists.
func (a *ACL) GetUser(name string) (*User, bool) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	u, ok := a.users[name]
	return u, ok
}

// DelUser removes a user. The default user cannot be deleted.
func (a *ACL) DelUser(name string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if name == "default" {
		return false
	}
	if _, ok := a.users[name]; !ok {
		return false
	}
	delete(a.users, name)
	return true
}

// Users returns the names of all registered users.
func (a *ACL) Users() []string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	names := make([]string, 0, len(a.users))
	for name := range a.users {
		names = append(names, name)
	}
	return names
}

// List returns all registered users.
func (a *ACL) List() []*User {
	a.mu.RLock()
	defer a.mu.RUnlock()
	users := make([]*User, 0, len(a.users))
	for _, u := range a.users {
		users = append(users, u)
	}
	return users
}

// Authenticate checks username/password and returns the user on success.
func (a *ACL) Authenticate(name, password string) (*User, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	u, ok := a.users[name]
	if !ok {
		a.addLog("auth", name, "", "User not found")
		return nil, fmt.Errorf("WRONGPASS invalid username-password pair or user is disabled.")
	}
	if !u.Enabled {
		a.addLog("auth", name, "", "User disabled")
		return nil, fmt.Errorf("WRONGPASS invalid username-password pair or user is disabled.")
	}
	if !u.CheckPassword(password) {
		a.addLog("auth", name, "", "Wrong password")
		return nil, fmt.Errorf("WRONGPASS invalid username-password pair or user is disabled.")
	}
	return u, nil
}

func (a *ACL) addLog(reason, user, clientAddr, details string) {
	entry := LogEntry{
		Timestamp:  time.Now().UnixMilli(),
		Reason:     reason,
		User:       user,
		ClientAddr: clientAddr,
		Details:    details,
	}
	a.log = append(a.log, entry)
	if len(a.log) > a.logSize {
		a.log = a.log[1:]
	}
}

// Log returns the last count log entries.
func (a *ACL) Log(count int) []LogEntry {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if count <= 0 || count > len(a.log) {
		count = len(a.log)
	}
	start := len(a.log) - count
	result := make([]LogEntry, count)
	copy(result, a.log[start:])
	return result
}

// ResetLog clears the ACL log.
func (a *ACL) ResetLog() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.log = nil
}

// DefaultUser returns the default user.
func (a *ACL) DefaultUser() *User {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.users["default"]
}
