package main

import (
	"context"
	"fmt"
	"net"
	"os"

	"github.com/amemiya02/hayakv/config"
	"github.com/amemiya02/hayakv/internal/iface"
	"github.com/amemiya02/hayakv/internal/lib/logger"
	"github.com/amemiya02/hayakv/internal/lib/utils"
	stdserver "github.com/amemiya02/hayakv/internal/net/goroutine"
	"github.com/amemiya02/hayakv/internal/rediscluster"
	"github.com/amemiya02/hayakv/internal/server"
)

var banner = `
  ██╗  ██╗  █████╗  ██╗   ██╗  █████╗  ██╗  ██╗ ██╗    ██╗
  ██║  ██║ ██╔══██╗ ╚██╗ ██╔╝ ██╔══██╗ ██║ ██╔╝╚██╗  ██╔╝
  ███████║ ███████║  ╚████╔╝  ███████║ █████╔╝  ╚██╗██╔╝
  ██╔══██║ ██╔══██║   ╚██╔╝   ██╔══██║ ██╔═██╗   ╚████╔╝
  ██║  ██║ ██║  ██║    ██║    ██║  ██║ ██║ ╚██╗   ╚██╔╝
  ╚═╝  ╚═╝ ╚═╝  ╚═╝    ╚═╝    ╚═╝  ╚═╝ ╚═╝  ╚═╝    ╚╝
`

var defaultProperties = &config.ServerProperties{
	Bind:           "0.0.0.0",
	Port:           6379,
	AppendOnly:     false,
	AppendFilename: "",
	MaxClients:     1000,
	RunID:          utils.RandString(40),
	NetBackend:     "goroutine",
	EngineBackend:  "shardmap",
	ProtoMax:       "resp2",
}

func fileExists(filename string) bool {
	info, err := os.Stat(filename)
	return err == nil && !info.IsDir()
}

func main() {
	print(banner)
	logger.Setup(&logger.Settings{
		Path:       "logs",
		Name:       "hayakv",
		Ext:        "log",
		TimeFormat: "2006-01-02",
	})
	configFilename := os.Getenv("CONFIG")
	if configFilename == "" {
		if fileExists("redis.conf") {
			config.SetupConfig("redis.conf")
		} else {
			config.Properties = defaultProperties
		}
	} else {
		config.SetupConfig(configFilename)
	}
	config.ApplyArgvOverrides(config.Properties, os.Args[1:])
	listenAddr := fmt.Sprintf("%s:%d", config.Properties.Bind, config.Properties.Port)

	server.NormalizeBackends(config.Properties)

	engine, err := server.NewStorageEngine(config.Properties)
	if err != nil {
		msg := fmt.Sprintf("configure storage engine failed: %v", err)
		logger.Errorf("%s", msg)
		fmt.Fprintln(os.Stderr, msg)
		return
	}

	engine, err = server.MaybeWrapCluster(config.Properties, engine)
	if err != nil {
		msg := fmt.Sprintf("configure cluster failed: %v", err)
		logger.Errorf("%s", msg)
		fmt.Fprintln(os.Stderr, msg)
		return
	}

	if ce, ok := engine.(*rediscluster.ClusterEngine); ok {
		if err := ce.StartBus(); err != nil {
			logger.Errorf("cluster bus failed: %v", err)
			fmt.Fprintln(os.Stderr, "cluster bus failed:", err)
			return
		}
	}

	netServer, err := server.NewNetServerWithEngine(config.Properties, engine)
	if err != nil {
		msg := fmt.Sprintf("configure net server failed: %v", err)
		logger.Errorf("%s", msg)
		fmt.Fprintln(os.Stderr, msg)
		return
	}

	codec, err := server.NewProtocolCodec(config.Properties)
	if err != nil {
		msg := fmt.Sprintf("configure protocol codec failed: %v", err)
		logger.Errorf("%s", msg)
		fmt.Fprintln(os.Stderr, msg)
		return
	}

	// For the eventloop backend, set the codec on the server directly.
	if elServer, ok := netServer.(interface{ SetCodec(iface.ProtocolCodec) }); ok {
		elServer.SetCodec(codec)
	}

	handler := stdserver.NewHandlerWithDB(engine, codec)
	ctx := context.Background()

	logger.Info("Ready to accept connections tcp")

	// Start TLS listener if configured.
	if config.Properties.TLSPort > 0 {
		tlsAddr := fmt.Sprintf("%s:%d", config.Properties.Bind, config.Properties.TLSPort)
		tlsHandler := stdserver.NewHandlerWithDB(engine, codec)
		go func() {
			logger.Info("TLS server listening on " + tlsAddr)
			if err := stdserver.ServeTLS(tlsAddr, tlsHandler,
				config.Properties.TLSCertFile,
				config.Properties.TLSKeyFile,
				config.Properties.TLSCACertFile); err != nil {
				logger.Error("TLS server error: " + err.Error())
			}
		}()
	}

	// Start unix socket listener if configured.
	if sock := config.Properties.UnixSocket; sock != "" {
		go func() {
			_ = os.Remove(sock) // remove stale socket file
			ln, err := net.Listen("unix", sock)
			if err != nil {
				logger.Errorf("unix socket listen %s failed: %v", sock, err)
				return
			}
			logger.Infof("Listening on unix socket %s", sock)
			for {
				conn, err := ln.Accept()
				if err != nil {
					logger.Errorf("unix socket accept: %v", err)
					return
				}
				go handler.Handle(ctx, conn)
			}
		}()
	}

	err = netServer.Run(ctx, listenAddr, handler)
	if err != nil {
		logger.Errorf("start server failed: %v", err)
	}
}
