package std

/*
 * A tcp.Handler implements redis protocol
 */

import (
	"context"
	"io"
	"net"
	"strings"
	"sync"

	"github.com/amemiya02/hayakv/config"
	"github.com/amemiya02/hayakv/internal/cluster"
	"github.com/amemiya02/hayakv/internal/command"
	"github.com/amemiya02/hayakv/internal/iface"
	"github.com/amemiya02/hayakv/internal/lib/logger"
	"github.com/amemiya02/hayakv/internal/lib/sync/atomic"
	"github.com/amemiya02/hayakv/internal/net/goroutine/tcp"
	"github.com/amemiya02/hayakv/internal/proto/resp2/parser"
	"github.com/amemiya02/hayakv/internal/proto/resp2/protocol"
	"github.com/amemiya02/hayakv/internal/server/connection"
)

var (
	unknownErrReplyBytes = []byte("-ERR unknown\r\n")
)

// Handler implements tcp.Handler and serves as a redis server
type Handler struct {
	activeConn sync.Map // *client -> placeholder
	db         iface.StorageEngine
	closing    atomic.Boolean // refusing new client and new request
}

// NewHandlerWithDB creates a Handler with an injected storage engine
func NewHandlerWithDB(db iface.StorageEngine) *Handler {
	return &Handler{db: db}
}

// MakeHandler creates a Handler instance
func MakeHandler() *Handler {
	var db iface.StorageEngine
	if config.Properties.ClusterEnable {
		db = cluster.MakeCluster()
	} else {
		db = database.NewStandaloneServer()
	}
	return NewHandlerWithDB(db)
}

func Serve(addr string, handler *Handler) error {
	return tcp.ListenAndServeWithSignal(&tcp.Config{
		Address: addr,
	}, handler)
}

func (h *Handler) closeClient(client *connection.Connection) {
	_ = client.Close()
	h.db.AfterClientClose(client)
	h.activeConn.Delete(client)
}

// Handle receives and executes redis commands
func (h *Handler) Handle(ctx context.Context, conn net.Conn) {
	if h.closing.Get() {
		// closing handler refuse new connection
		_ = conn.Close()
		return
	}

	client := connection.NewConn(conn)
	h.activeConn.Store(client, struct{}{})

	ch := parser.ParseStream(conn)
	for payload := range ch {
		if payload.Err != nil {
			if payload.Err == io.EOF ||
				payload.Err == io.ErrUnexpectedEOF ||
				strings.Contains(payload.Err.Error(), "use of closed network connection") {
				// connection closed
				h.closeClient(client)
				logger.Info("connection closed: " + client.RemoteAddr())
				return
			}
			// protocol err
			errReply := protocol.MakeErrReply(payload.Err.Error())
			_, err := client.Write(errReply.ToBytes())
			if err != nil {
				h.closeClient(client)
				logger.Info("connection closed: " + client.RemoteAddr())
				return
			}
			continue
		}
		if payload.Data == nil {
			logger.Error("empty payload")
			continue
		}
		r, ok := payload.Data.(*protocol.MultiBulkReply)
		if !ok {
			logger.Error("require multi bulk protocol")
			continue
		}
		result := h.db.Exec(client, r.Args)
		if result != nil {
			_, _ = client.Write(result.ToBytes())
		} else {
			_, _ = client.Write(unknownErrReplyBytes)
		}
	}
}

// Close stops handler
func (h *Handler) Close() error {
	logger.Info("handler shutting down...")
	h.closing.Set(true)
	// TODO: concurrent wait
	h.activeConn.Range(func(key interface{}, val interface{}) bool {
		client := key.(*connection.Connection)
		_ = client.Close()
		return true
	})
	h.db.Close()
	return nil
}

// Server implements iface.NetServer for the goroutine-based TCP server
type Server struct{}

// NewServer creates a new Server instance
func NewServer() *Server {
	return &Server{}
}

// Run starts the TCP server with the given handler
func (s *Server) Run(ctx context.Context, addr string, handler iface.NetHandler) error {
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	closeChan := make(chan struct{})
	go func() {
		<-ctx.Done()
		close(closeChan)
	}()
	tcp.ListenAndServe(listener, handler, closeChan)
	return nil
}

// Close is a no-op for the goroutine server
func (s *Server) Close() error {
	return nil
}
