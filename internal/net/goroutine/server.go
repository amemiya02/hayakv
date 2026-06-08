package std

/*
 * A tcp.Handler implements redis protocol
 */

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"

	"github.com/amemiya02/hayakv/config"
	"github.com/amemiya02/hayakv/internal/cluster"
	"github.com/amemiya02/hayakv/internal/command"
	"github.com/amemiya02/hayakv/internal/iface"
	"github.com/amemiya02/hayakv/internal/lib/logger"
	"github.com/amemiya02/hayakv/internal/lib/sync/atomic"
	"github.com/amemiya02/hayakv/internal/net/goroutine/tcp"
	"github.com/amemiya02/hayakv/internal/proto/resp2"
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
	codec      iface.ProtocolCodec
	closing    atomic.Boolean // refusing new client and new request
}

// NewHandlerWithDB creates a Handler with an injected storage engine and protocol codec
func NewHandlerWithDB(db iface.StorageEngine, codec iface.ProtocolCodec) *Handler {
	return &Handler{db: db, codec: codec}
}

// MakeHandler creates a Handler instance
func MakeHandler() *Handler {
	var db iface.StorageEngine
	if config.Properties.ClusterEnable {
		db = cluster.MakeCluster()
	} else {
		db = database.NewStandaloneServer()
	}
	codec := resp2.Codec{}
	return NewHandlerWithDB(db, codec)
}

func Serve(addr string, handler *Handler) error {
	return tcp.ListenAndServeWithSignal(&tcp.Config{
		Address: addr,
	}, handler)
}

// ServeTLS starts a TLS listener on the given address using the provided cert/key
// files.  If caCertFile is non-empty it enables mutual TLS (client certificate
// verification).  The function blocks until a shutdown signal is received.
func ServeTLS(addr string, handler *Handler, certFile, keyFile, caCertFile string) error {
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return fmt.Errorf("load TLS cert/key: %w", err)
	}

	tlsCfg := &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS12,
	}

	if caCertFile != "" {
		caCert, err := os.ReadFile(caCertFile)
		if err != nil {
			return fmt.Errorf("load CA cert: %w", err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(caCert) {
			return fmt.Errorf("failed to parse CA cert")
		}
		tlsCfg.ClientCAs = pool
		tlsCfg.ClientAuth = tls.RequireAndVerifyClientCert
	}

	ln, err := tls.Listen("tcp", addr, tlsCfg)
	if err != nil {
		return fmt.Errorf("TLS listen: %w", err)
	}

	logger.Info(fmt.Sprintf("bind: %s, start TLS listening...", addr))

	closeChan := make(chan struct{})
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGHUP, syscall.SIGQUIT, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		sig := <-sigCh
		switch sig {
		case syscall.SIGHUP, syscall.SIGQUIT, syscall.SIGTERM, syscall.SIGINT:
			closeChan <- struct{}{}
		}
	}()

	tcp.ServeListener(ln, handler, closeChan)
	return nil
}

func (h *Handler) closeClient(client *connection.Connection) {
	h.activeConn.Delete(client)
	_ = client.Close()
}

// Handle receives and executes redis commands
func (h *Handler) Handle(ctx context.Context, conn net.Conn) {
	if h.closing.Get() {
		// closing handler refuse new connection
		_ = conn.Close()
		return
	}

	client := connection.NewConn(conn)
	client.OnClose(func(c *connection.Connection) {
		h.db.AfterClientClose(c)
	})
	h.activeConn.Store(client, struct{}{})

	ch := h.codec.DecodeStream(conn)
	for payload := range ch {
		if payload.Err != nil {
			if payload.Err == io.EOF ||
				payload.Err == io.ErrUnexpectedEOF ||
				strings.Contains(payload.Err.Error(), "use of closed network connection") {
				h.closeClient(client)
				logger.Info("connection closed: " + client.RemoteAddr())
				return
			}
			errReply := protocol.MakeErrReply(payload.Err.Error())
			_, err := client.Write(h.codec.Encode(errReply, client.Protocol()))
			if err != nil {
				h.closeClient(client)
				logger.Info("connection closed: " + client.RemoteAddr())
				return
			}
			continue
		}
		if payload.Reply == nil {
			logger.Error("empty payload")
			continue
		}
		r, ok := payload.Reply.(*protocol.MultiBulkReply)
		if !ok {
			logger.Error("require multi bulk protocol")
			continue
		}
		result := h.db.Exec(client, r.Args)
		if result != nil {
			_, _ = client.Write(h.codec.Encode(result, client.Protocol()))
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
		h.closeClient(client)
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
