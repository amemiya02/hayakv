package acl

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	"github.com/amemiya02/hayakv/internal/lib/wildcard"
)

type keyPattern struct {
	pat         string
	read, write bool
}

// User represents a Redis ACL user with permissions for commands, keys, and channels.
type User struct {
	Name       string
	Enabled    bool
	NoPass     bool
	PassHashes map[string]struct{}
	AllKeys    bool
	KeyPats    []keyPattern
	AllChans   bool
	ChanPats   []string
	AllCmds    bool
	cmdAllow   map[string]bool
	catLookup  func(cat string) []string
}

// NewUser creates a new User with default (no) permissions.
func NewUser(name string) *User {
	return &User{
		Name:       name,
		PassHashes: map[string]struct{}{},
		cmdAllow:   map[string]bool{},
		catLookup:  func(string) []string { return nil },
	}
}

// SetCategoryLookup sets the function used to resolve ACL category names to command lists.
func (u *User) SetCategoryLookup(f func(string) []string) { u.catLookup = f }

func sha256hex(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

// CheckPassword returns true if the given plaintext password matches any stored hash,
// or if the user has NoPass set.
func (u *User) CheckPassword(pw string) bool {
	if u.NoPass {
		return true
	}
	_, ok := u.PassHashes[sha256hex(pw)]
	return ok
}

// ApplyRules applies a list of ACL rule tokens to the user.
func (u *User) ApplyRules(rules []string) error {
	for _, r := range rules {
		switch {
		case r == "on":
			u.Enabled = true
		case r == "off":
			u.Enabled = false
		case r == "nopass":
			u.NoPass = true
			u.PassHashes = map[string]struct{}{}
		case r == "resetpass":
			u.NoPass = false
			u.PassHashes = map[string]struct{}{}
		case strings.HasPrefix(r, ">"):
			u.NoPass = false
			u.PassHashes[sha256hex(r[1:])] = struct{}{}
		case strings.HasPrefix(r, "#"):
			u.PassHashes[strings.ToLower(r[1:])] = struct{}{}
		case r == "~*" || r == "allkeys":
			u.AllKeys = true
		case strings.HasPrefix(r, "%R~"):
			u.KeyPats = append(u.KeyPats, keyPattern{pat: r[3:], read: true})
		case strings.HasPrefix(r, "%W~"):
			u.KeyPats = append(u.KeyPats, keyPattern{pat: r[3:], write: true})
		case strings.HasPrefix(r, "~"):
			u.KeyPats = append(u.KeyPats, keyPattern{pat: r[1:], read: true, write: true})
		case r == "&*" || r == "allchannels":
			u.AllChans = true
		case strings.HasPrefix(r, "&"):
			u.ChanPats = append(u.ChanPats, r[1:])
		case r == "+@all" || r == "allcommands":
			u.AllCmds = true
		case r == "-@all" || r == "nocommands":
			u.AllCmds = false
			u.cmdAllow = map[string]bool{}
		case strings.HasPrefix(r, "+@"):
			for _, c := range u.catLookup(r[2:]) {
				u.cmdAllow[c] = true
			}
		case strings.HasPrefix(r, "-@"):
			for _, c := range u.catLookup(r[2:]) {
				u.cmdAllow[c] = false
			}
		case strings.HasPrefix(r, "+"):
			u.cmdAllow[strings.ToLower(r[1:])] = true
		case strings.HasPrefix(r, "-"):
			u.cmdAllow[strings.ToLower(r[1:])] = false
		case r == "reset":
			n := u.Name
			*u = *NewUser(n)
		default:
			return fmt.Errorf("ERR Error in ACL SETUSER modifier '%s'", r)
		}
	}
	return nil
}

// CanRunCommand returns true if the user is allowed to run the named command.
func (u *User) CanRunCommand(name string) bool {
	name = strings.ToLower(name)
	if v, ok := u.cmdAllow[name]; ok {
		return v
	}
	return u.AllCmds
}

// CanAccessKey returns true if the user can access the given key for read or write.
func (u *User) CanAccessKey(key string, write bool) bool {
	if u.AllKeys {
		return true
	}
	for _, p := range u.KeyPats {
		if (write && !p.write) || (!write && !p.read) {
			continue
		}
		if m, err := wildcard.CompilePattern(p.pat); err == nil && m.IsMatch(key) {
			return true
		}
	}
	return false
}

// CanAccessChannel returns true if the user can access the given pub/sub channel.
func (u *User) CanAccessChannel(ch string) bool {
	if u.AllChans {
		return true
	}
	for _, p := range u.ChanPats {
		if m, err := wildcard.CompilePattern(p); err == nil && m.IsMatch(ch) {
			return true
		}
	}
	return false
}

// IsDefaultAllPerms returns true if this user has full permissions (default user fast path).
func (u *User) IsDefaultAllPerms() bool {
	return u.Name == "default" && u.Enabled && u.AllCmds && u.AllKeys && u.AllChans
}

// DescribeRules serializes the user's rules back to ACL SETUSER format.
func (u *User) DescribeRules() []string {
	var rules []string
	if u.Enabled {
		rules = append(rules, "on")
	} else {
		rules = append(rules, "off")
	}
	if u.NoPass {
		rules = append(rules, "nopass")
	}
	for h := range u.PassHashes {
		rules = append(rules, "#"+h)
	}
	if u.AllKeys {
		rules = append(rules, "~*")
	}
	for _, p := range u.KeyPats {
		prefix := "~"
		if p.read && !p.write {
			prefix = "%R~"
		}
		if p.write && !p.read {
			prefix = "%W~"
		}
		rules = append(rules, prefix+p.pat)
	}
	if u.AllChans {
		rules = append(rules, "&*")
	}
	for _, p := range u.ChanPats {
		rules = append(rules, "&"+p)
	}
	if u.AllCmds {
		rules = append(rules, "+@all")
	}
	// Sort command permissions for deterministic output
	var allows, denies []string
	for cmd, allowed := range u.cmdAllow {
		if allowed {
			allows = append(allows, "+"+cmd)
		} else {
			denies = append(denies, "-"+cmd)
		}
	}
	sort.Strings(allows)
	sort.Strings(denies)
	rules = append(rules, allows...)
	rules = append(rules, denies...)
	return rules
}
