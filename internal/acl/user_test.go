package acl

import "testing"

func TestUserParseRules(t *testing.T) {
	u := NewUser("alice")
	u.SetCategoryLookup(func(cat string) []string {
		if cat == "read" {
			return []string{"get"}
		}
		return nil
	})
	if err := u.ApplyRules([]string{"on", ">secret", "~app:*", "+@read", "+set", "&ch:*"}); err != nil {
		t.Fatal(err)
	}
	if !u.Enabled || !u.CheckPassword("secret") {
		t.Fatal("on/password not applied")
	}
	if !u.CanAccessKey("app:1", false) || u.CanAccessKey("other", false) {
		t.Fatal("key pattern ~app:* wrong")
	}
	if !u.CanRunCommand("get") || !u.CanRunCommand("set") || u.CanRunCommand("del") {
		t.Fatal("command perms wrong (+@read +set; del denied)")
	}
	if !u.CanAccessChannel("ch:news") || u.CanAccessChannel("other") {
		t.Fatal("channel pattern &ch:* wrong")
	}
}

func TestUserDescribeRules(t *testing.T) {
	u := NewUser("bob")
	u.SetCategoryLookup(func(cat string) []string {
		if cat == "read" {
			return []string{"get"}
		}
		return nil
	})
	u.ApplyRules([]string{"on", ">pw", "~k:*", "+@read"})
	rules := u.DescribeRules()
	found := false
	for _, r := range rules {
		if r == "+get" {
			found = true
		}
	}
	if !found {
		t.Fatalf("DescribeRules missing +get: %v", rules)
	}
}
