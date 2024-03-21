package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"github.com/alecthomas/kong"
	"github.com/jonasohland/consul-registrator/pkg/registrator"
	"github.com/jonasohland/consul-registrator/pkg/server"
	htx "github.com/jonasohland/ext/http"
	slx "github.com/jonasohland/slog-ext/pkg/slog-ext"
)

type Cmd struct {
	Listen     string `short:"l" help:"Listen on this address for http requests"`
	ConfigDir  []string
	ConfigFile []string
}

func main() {
	runtime.GOMAXPROCS(1)

	cmd := new(Cmd)
	kong.Parse(cmd)

	log := slog.New(slx.NewHandler(os.Stderr, slog.LevelDebug))
	ctx, cancel := context.WithCancel(slx.WithContext(context.Background(), log))

	quit := make(chan os.Signal, 1)
	reload := make(chan os.Signal, 1)

	signal.Notify(quit, syscall.SIGTERM, syscall.SIGINT)
	signal.Notify(reload, syscall.SIGHUP)

	go func() {
		signal := <-quit
		fmt.Fprintln(os.Stderr, signal)
		cancel()
	}()

	config := &registrator.Config{
		Ctx: ctx,
		Sources: registrator.SourceConfig{
			ConfigDirectories: cmd.ConfigDir,
			ConfigFiles:       cmd.ConfigFile,
		},
	}

	registrator, err := registrator.NewRegistrator(config)
	if err != nil {
		log.Error("could not start registrator", "error", err)
		os.Exit(1)
	}

	go func() {
		for {
			<-reload
			if err := registrator.Reload(ctx); err != nil {
				log.Error("reload", "error", err)
			}
		}
	}()

	server, err := htx.NewContextServer(ctx, server.NewHandler(registrator), "tcp", cmd.Listen)
	if err != nil {
		log.Error("could not start http server", "error", err)
		os.Exit(1)
	}

	<-ctx.Done()

	if err := registrator.Wait(time.Second * 5); err != nil {
		log.Error("graceful shutdown failed", "error", err)
		os.Exit(1)
	}

	if err := server.Wait(time.Second * 5); err != nil {
		log.Error("http server shutdown failed", "error", err)
		os.Exit(1)
	}

	log.Info("shutdown complete")
}
