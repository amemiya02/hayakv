package database

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	"github.com/amemiya02/hayakv/internal/acl"
	"github.com/amemiya02/hayakv/internal/iface/redis"
	"github.com/amemiya02/hayakv/internal/proto/resp2/protocol"
)

func init() {
	registerSpecialCommand("Acl", -2, 0).
		attachCommandExtra([]string{redisFlagAdmin, redisFlagNoScript}, 0, 0, 0)
}

// execACL dispatches ACL subcommands.
func execACL(db *DB, c redis.Connection, args [][]byte) redis.Reply {
	if len(args) < 1 {
		return protocol.MakeErrReply("ERR wrong number of arguments for 'acl' command")
	}
	sub := strings.ToLower(string(args[0]))
	switch sub {
	case "setuser":
		return execACLSetUser(db, args[1:])
	case "getuser":
		return execACLGetUser(db, args[1:])
	case "deluser":
		return execACLDelUser(db, args[1:])
	case "whoami":
		return execACLWhoami(db, c, args[1:])
	case "users":
		return execACLUsers(db)
	case "list":
		return execACLList(db)
	case "cat":
		return execACLCat(db, args[1:])
	case "genpass":
		return execACLGenPass(db, args[1:])
	case "log":
		return execACLLog(db, args[1:])
	case "load":
		return execACLLoad(db, args[1:])
	case "save":
		return execACLSave(db, args[1:])
	case "help":
		return execACLHelp()
	default:
		return protocol.MakeErrReply("ERR Unknown subcommand or wrong number of arguments for '" + sub + "'")
	}
}

// execACLSetUser creates or modifies a user.
// ACL SETUSER <username> [<rule> ...]
func execACLSetUser(db *DB, args [][]byte) redis.Reply {
	if len(args) < 1 {
		return protocol.MakeErrReply("ERR wrong number of arguments for 'acl|setuser' command")
	}
	name := string(args[0])
	rules := make([]string, len(args)-1)
	for i, a := range args[1:] {
		rules[i] = string(a)
	}
	if err := db.server.acl.SetUser(name, rules); err != nil {
		return protocol.MakeErrReply(err.Error())
	}
	return &protocol.OkReply{}
}

// execACLGetUser returns the rules for a user.
// ACL GETUSER <username>
func execACLGetUser(db *DB, args [][]byte) redis.Reply {
	if len(args) != 1 {
		return protocol.MakeErrReply("ERR wrong number of arguments for 'acl|getuser' command")
	}
	name := string(args[0])
	u, ok := db.server.acl.GetUser(name)
	if !ok {
		return protocol.MakeNullBulkReply()
	}
	// Redis 8 returns a structured map (flags, passwords, commands, keys, channels)
	var pairs []redis.Reply
	// flags
	flags := []redis.Reply{}
	if u.Enabled {
		flags = append(flags, protocol.MakeBulkReply([]byte("on")))
	} else {
		flags = append(flags, protocol.MakeBulkReply([]byte("off")))
	}
	if u.NoPass {
		flags = append(flags, protocol.MakeBulkReply([]byte("nopass")))
	}
	if u.AllKeys {
		flags = append(flags, protocol.MakeBulkReply([]byte("allkeys")))
	}
	if u.AllChans {
		flags = append(flags, protocol.MakeBulkReply([]byte("allchannels")))
	}
	if u.AllCmds {
		flags = append(flags, protocol.MakeBulkReply([]byte("allcommands")))
	}
	pairs = append(pairs,
		protocol.MakeBulkReply([]byte("flags")),
		protocol.MakeMultiRawReply(flags))
	// passwords
	passwords := make([]redis.Reply, 0, len(u.PassHashes))
	for h := range u.PassHashes {
		passwords = append(passwords, protocol.MakeBulkReply([]byte(h)))
	}
	pairs = append(pairs,
		protocol.MakeBulkReply([]byte("passwords")),
		protocol.MakeMultiRawReply(passwords))
	// commands
	rules := u.DescribeRules()
	elems := make([]redis.Reply, len(rules))
	for i, r := range rules {
		elems[i] = protocol.MakeBulkReply([]byte(r))
	}
	pairs = append(pairs,
		protocol.MakeBulkReply([]byte("commands")),
		protocol.MakeMultiRawReply(elems))
	return protocol.MakeMultiRawReply(pairs)
}

// execACLDelUser removes one or more users.
// ACL DELUSER <username> [<username> ...]
func execACLDelUser(db *DB, args [][]byte) redis.Reply {
	if len(args) < 1 {
		return protocol.MakeErrReply("ERR wrong number of arguments for 'acl|deluser' command")
	}
	var count int64
	for _, a := range args {
		if db.server.acl.DelUser(string(a)) {
			count++
		}
	}
	return protocol.MakeIntReply(count)
}

// execACLWhoami returns the authenticated user name.
func execACLWhoami(db *DB, c redis.Connection, args [][]byte) redis.Reply {
	if len(args) != 0 {
		return protocol.MakeErrReply("ERR wrong number of arguments for 'acl|whoami' command")
	}
	// Return the connection's actual authenticated user name.
	if c != nil {
		if u := c.User(); u != nil {
			if user, ok := u.(*acl.User); ok {
				return protocol.MakeBulkReply([]byte(user.Name))
			}
		}
	}
	return protocol.MakeBulkReply([]byte("default"))
}

// execACLUsers returns all user names.
func execACLUsers(db *DB) redis.Reply {
	names := db.server.acl.Users()
	sort.Strings(names)
	elems := make([]redis.Reply, len(names))
	for i, n := range names {
		elems[i] = protocol.MakeBulkReply([]byte(n))
	}
	return protocol.MakeMultiRawReply(elems)
}

// execACLList returns each user as "user <name> <rules...>".
func execACLList(db *DB) redis.Reply {
	users := db.server.acl.List()
	lines := make([]redis.Reply, 0, len(users))
	for _, u := range users {
		parts := []string{"user", u.Name}
		parts = append(parts, u.DescribeRules()...)
		lines = append(lines, protocol.MakeBulkReply([]byte(strings.Join(parts, " "))))
	}
	return protocol.MakeMultiRawReply(lines)
}

// execACLCat returns commands in a category, or lists all categories.
func execACLCat(db *DB, args [][]byte) redis.Reply {
	if len(args) == 0 {
		// List all categories.
		cats := make([]string, len(acl.KnownCategories))
		copy(cats, acl.KnownCategories)
		sort.Strings(cats)
		elems := make([]redis.Reply, len(cats))
		for i, c := range cats {
			elems[i] = protocol.MakeBulkReply([]byte(c))
		}
		return protocol.MakeMultiRawReply(elems)
	}
	catName := strings.ToLower(string(args[0]))
	cmds, ok := acl.CategoryCommands[catName]
	if !ok {
		return protocol.MakeErrReply("ERR Unknown category '" + catName + "'")
	}
	sort.Strings(cmds)
	elems := make([]redis.Reply, len(cmds))
	for i, c := range cmds {
		elems[i] = protocol.MakeBulkReply([]byte(c))
	}
	return protocol.MakeMultiRawReply(elems)
}

// execACLGenPass generates a random hex password.
func execACLGenPass(db *DB, args [][]byte) redis.Reply {
	bits := 256
	if len(args) == 1 {
		n, err := parsePositiveInt(string(args[0]))
		if err != nil || n < 1 || n > 4096 {
			return protocol.MakeErrReply("ERR ACL GENPASS supports only values between 1 and 4096")
		}
		bits = n
	}
	bytesLen := (bits + 7) / 8
	buf := make([]byte, bytesLen)
	_, _ = rand.Read(buf)
	return protocol.MakeBulkReply([]byte(hex.EncodeToString(buf)))
}

// execACLLog returns recent ACL log entries.
func execACLLog(db *DB, args [][]byte) redis.Reply {
	count := 10
	if len(args) == 1 {
		n, err := parsePositiveInt(string(args[0]))
		if err != nil {
			return protocol.MakeErrReply("ERR value is not an integer or out of range")
		}
		count = n
	} else if len(args) > 1 {
		return protocol.MakeErrReply("ERR wrong number of arguments for 'acl|log' command")
	}
	entries := db.server.acl.Log(count)
	elems := make([]redis.Reply, len(entries))
	for i, e := range entries {
		fields := []redis.Reply{
			protocol.MakeBulkReply([]byte("reason")),
			protocol.MakeBulkReply([]byte(e.Reason)),
			protocol.MakeBulkReply([]byte("user")),
			protocol.MakeBulkReply([]byte(e.User)),
			protocol.MakeBulkReply([]byte("client-address")),
			protocol.MakeBulkReply([]byte(e.ClientAddr)),
			protocol.MakeBulkReply([]byte("details")),
			protocol.MakeBulkReply([]byte(e.Details)),
			protocol.MakeBulkReply([]byte("timestamp")),
			protocol.MakeIntReply(e.Timestamp),
		}
		elems[i] = protocol.MakeMultiRawReply(fields)
	}
	return protocol.MakeMultiRawReply(elems)
}

// execACLLoad loads the ACL file (stub).
func execACLLoad(db *DB, args [][]byte) redis.Reply {
	// TODO: implement ACL LOAD from aclfile
	return protocol.MakeErrReply("ERR ACL LOAD not yet implemented")
}

// execACLSave saves the ACL file (stub).
func execACLSave(db *DB, args [][]byte) redis.Reply {
	// TODO: implement ACL SAVE to aclfile
	return protocol.MakeErrReply("ERR ACL SAVE not yet implemented")
}

// execACLHelp returns help text for the ACL command.
func execACLHelp() redis.Reply {
	lines := []string{
		"ACL <subcommand> [<arg> [value] [opt] ...]. Subcommands are:",
		"SETUSER <username> [<rule> ...]",
		"    Create or modify a user.",
		"GETUSER <username>",
		"    Get the ACL details of a user.",
		"DELUSER <username> [<username> ...]",
		"    Delete users.",
		"USERS",
		"    List all registered users.",
		"LIST",
		"    List users with their rules.",
		"WHOAMI",
		"    Return the authenticated user name.",
		"[GENPASS [<bits>]]",
		"    Generate a random password.",
		"CAT [<category>]",
		"    List available ACL categories, or commands in a category.",
		"LOG [<count>]",
		"    Show the ACL log.",
		"LOAD",
		"    Reload users from the ACL file.",
		"SAVE",
		"    Save the current ACL rules to the ACL file.",
		"HELP",
		"    Show this help.",
	}
	elems := make([]redis.Reply, len(lines))
	for i, l := range lines {
		elems[i] = protocol.MakeBulkReply([]byte(l))
	}
	return protocol.MakeMultiRawReply(elems)
}

// parsePositiveInt parses a positive integer from a string.
func parsePositiveInt(s string) (int, error) {
	var n int
	_, err := fmt.Sscanf(s, "%d", &n)
	if err != nil {
		return 0, err
	}
	if n <= 0 {
		return 0, fmt.Errorf("not positive")
	}
	return n, nil
}
