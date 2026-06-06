package server

import (
	"sync"
	"testing"

	"github.com/amemiya02/hayakv/internal/iface"
	"github.com/amemiya02/hayakv/internal/iface/redis"
	"github.com/amemiya02/hayakv/internal/proto/resp2/protocol"
)

// mockEngine is a simple StorageEngine for testing.
type mockEngine struct {
	mu      sync.Mutex
	actions []string
}

func (m *mockEngine) Exec(client redis.Connection, cmdLine iface.CmdLine) redis.Reply {
	m.mu.Lock()
	m.actions = append(m.actions, "exec")
	m.mu.Unlock()
	return &protocol.OkReply{}
}

func (m *mockEngine) AfterClientClose(client redis.Connection) {
	m.mu.Lock()
	m.actions = append(m.actions, "after")
	m.mu.Unlock()
}

func (m *mockEngine) Close() {
	m.mu.Lock()
	m.actions = append(m.actions, "close")
	m.mu.Unlock()
}

func TestLockedEngine_Exec(t *testing.T) {
	inner := &mockEngine{}
	le := NewLockedEngine(inner)
	reply := le.Exec(nil, [][]byte{[]byte("PING")})
	if reply == nil {
		t.Fatal("expected reply, got nil")
	}
	if len(inner.actions) != 1 || inner.actions[0] != "exec" {
		t.Errorf("expected [exec], got %v", inner.actions)
	}
}

func TestLockedEngine_Concurrent(t *testing.T) {
	inner := &mockEngine{}
	le := NewLockedEngine(inner)

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			le.Exec(nil, [][]byte{[]byte("PING")})
		}()
	}
	wg.Wait()
	if len(inner.actions) != 100 {
		t.Errorf("expected 100 actions, got %d", len(inner.actions))
	}
}

func TestLockedEngine_ImplementsInterface(t *testing.T) {
	var _ iface.StorageEngine = NewLockedEngine(&mockEngine{})
}
