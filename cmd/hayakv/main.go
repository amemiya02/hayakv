package main

import (
	"context"
	"fmt"
	"os"

	"github.com/amemiya02/hayakv/config"
	"github.com/amemiya02/hayakv/internal/iface"
	"github.com/amemiya02/hayakv/internal/lib/logger"
	"github.com/amemiya02/hayakv/internal/lib/utils"
	stdserver "github.com/amemiya02/hayakv/internal/net/goroutine"
	"github.com/amemiya02/hayakv/internal/server"
)

var banner = `
  _                     _
 | |__   __ _ _   _  __| | __ ___      __
 | '_ \ / _' | | | |/ _' |/ _' \ \ /\ / /
 | | | | (_| | |_| | (_| | (_| |\ V  V /
 |_| |_|\__, |\__,_|\__,_|\__,_| \_/\_/
         |___/
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
	listenAddr := fmt.Sprintf("%s:%d", config.Properties.Bind, config.Properties.Port)

	server.NormalizeBackends(config.Properties)

	engine, err := server.NewStorageEngine(config.Properties)
	if err != nil {
		msg := fmt.Sprintf("configure storage engine failed: %v", err)
		logger.Errorf("%s", msg)
		fmt.Fprintln(os.Stderr, msg)
		return
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
	err = netServer.Run(ctx, listenAddr, handler)
	if err != nil {
		logger.Errorf("start server failed: %v", err)
	}
}
