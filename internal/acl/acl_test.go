package acl

import "testing"

func TestACLSetUserAuthenticate(t *testing.T) {
	a := NewACL()
	if err := a.SetUser("alice", []string{"on", ">secret", "~app:*", "+@read"}); err != nil {
		t.Fatal(err)
	}

	u, err := a.Authenticate("alice", "secret")
	if err != nil {
		t.Fatal(err)
	}
	if u.Name != "alice" {
		t.Fatal("wrong user")
	}

	_, err = a.Authenticate("alice", "wrong")
	if err == nil {
		t.Fatal("should fail with wrong password")
	}

	_, err = a.Authenticate("nobody", "x")
	if err == nil {
		t.Fatal("should fail for unknown user")
	}
}

func TestDefaultUser(t *testing.T) {
	a := NewACL()
	u, err := a.Authenticate("default", "")
	if err != nil {
		t.Fatal(err)
	}
	if !u.IsDefaultAllPerms() {
		t.Fatal("default should have all perms")
	}
}

func TestDelUser(t *testing.T) {
	a := NewACL()
	a.SetUser("bob", []string{"on", ">pw", "+get"})

	if !a.DelUser("bob") {
		t.Fatal("should delete bob")
	}
	if a.DelUser("bob") {
		t.Fatal("bob already deleted")
	}
	if a.DelUser("default") {
		t.Fatal("cannot delete default user")
	}
}

func TestDisabledUser(t *testing.T) {
	a := NewACL()
	a.SetUser("charlie", []string{"off", ">pw"})

	_, err := a.Authenticate("charlie", "pw")
	if err == nil {
		t.Fatal("should fail for disabled user")
	}
}

func TestLog(t *testing.T) {
	a := NewACL()
	// Trigger some auth failures to populate log.
	a.Authenticate("nobody", "x")
	a.Authenticate("nobody", "y")

	entries := a.Log(10)
	if len(entries) != 2 {
		t.Fatalf("expected 2 log entries, got %d", len(entries))
	}
	if entries[0].Reason != "auth" {
		t.Fatal("expected auth reason")
	}

	a.ResetLog()
	if len(a.Log(10)) != 0 {
		t.Fatal("log should be empty after reset")
	}
}
