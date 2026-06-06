package main

import (
	"context"
	"fmt"
	"os"

	"github.com/amemiya02/hayakv/config"
	"github.com/amemiya02/hayakv/internal/lib/logger"
	"github.com/amemiya02/hayakv/internal/lib/utils"
	stdserver "github.com/amemiya02/hayakv/internal/net/goroutine"
	"github.com/amemiya02/hayakv/internal/server"
)

var banner = `
   ______          ___
  / ____/___  ____/ (_)____
 / / __/ __ \/ __  / / ___/
/ /_/ / /_/ / /_/ / (__  )
\____/\____/\__,_/_/____/
`

var defaultProperties = &config.ServerProperties{
	Bind:           "0.0.0.0",
	Port:           6399,
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
		Name:       "godis",
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

	netServer, err := server.NewNetServer(config.Properties)
	if err != nil {
		msg := fmt.Sprintf("configure net server failed: %v", err)
		logger.Errorf("%s", msg)
		fmt.Fprintln(os.Stderr, msg)
		return
	}

	handler := stdserver.NewHandlerWithDB(engine)
	ctx := context.Background()
	err = netServer.Run(ctx, listenAddr, handler)
	if err != nil {
		logger.Errorf("start server failed: %v", err)
	}
}
