package database

import (
	"sort"
	"strings"
)

// commandACLCats maps command name to its list of ACL categories.
var commandACLCats = map[string][]string{}

func init() {
	for name, cmd := range cmdTable {
		cats := deriveACLCats(name, cmd)
		commandACLCats[name] = cats
		if cmd.extra == nil {
			cmd.extra = &commandExtra{}
		}
		cmd.extra.aclCats = cats
	}
}

func deriveACLCats(name string, cmd *command) []string {
	catSet := map[string]bool{}

	// From redis flags stored in commandExtra.signs
	if cmd.extra != nil {
		for _, flag := range cmd.extra.signs {
			switch flag {
			case redisFlagWrite:
				catSet["write"] = true
			case redisFlagReadonly:
				catSet["read"] = true
			case redisFlagAdmin:
				catSet["admin"] = true
				catSet["dangerous"] = true
			case redisFlagFast:
				catSet["fast"] = true
			case redisFlagPubSub:
				catSet["pubsub"] = true
			case redisFlagNoScript:
				catSet["scripting"] = true
			}
		}
	}

	// From command flags bitfield
	if cmd.flags&flagReadOnly != 0 {
		catSet["read"] = true
	}
	if cmd.flags == flagWrite {
		catSet["write"] = true
	}

	// Generic keyspace commands
	switch name {
	case "del", "unlink", "exists", "type", "rename", "renamenx", "expire", "expireat",
		"pexpire", "pexpireat", "ttl", "pttl", "persist", "copy", "dump", "restore",
		"keys", "scan", "sort", "object", "touch", "wait":
		catSet["generic"] = true
		catSet["read"] = true
	}

	// Connection commands
	switch name {
	case "auth", "hello", "ping", "echo", "select", "swapdb", "quit":
		catSet["connection"] = true
		catSet["fast"] = true
	}

	// Commands that are neither fast nor admin are "slow"
	if !catSet["fast"] && !catSet["admin"] {
		catSet["slow"] = true
	}

	cats := make([]string, 0, len(catSet))
	for c := range catSet {
		cats = append(cats, c)
	}
	sort.Strings(cats)
	return cats
}

// commandCategories returns the ACL categories for the named command.
func commandCategories(name string) []string {
	return commandACLCats[strings.ToLower(name)]
}

// commandsInCategory returns all command names belonging to the given ACL category.
func commandsInCategory(cat string) []string {
	var cmds []string
	for name, cats := range commandACLCats {
		for _, c := range cats {
			if c == cat {
				cmds = append(cmds, name)
				break
			}
		}
	}
	sort.Strings(cmds)
	return cmds
}
