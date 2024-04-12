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
	"github.com/hashicorp/consul/api"
	"github.com/jonasohland/consul-register/pkg/register"
	"github.com/jonasohland/consul-register/pkg/server"
	htx "github.com/jonasohland/ext/http"
	slx "github.com/jonasohland/slog-ext/pkg/slog-ext"
	"github.com/mattn/go-isatty"
)

var Version string

type Cmd struct {
	Listen     string   `short:"l" help:"Listen on this address for http requests"`
	ConfigDir  []string `short:"d" help:"Load configuration files from this directory, can be repeated"`
	ConfigFile []string `short:"c" help:"Load configuration from this file, can be repeated"`

	DeletePreparedQueries bool `help:"Do/Don't delete prepared queries on exit" negatable:""`

	Address    string `short:"a" help:"Consul Agent http address"`
	Token      string `short:"t" help:"Consul acl token"`
	CaCert     string `help:"Consul TLS CA certificate"`
	ClientCert string `help:"Consul TLS Client Certificate"`
	ClientKey  string `help:"Consul TLS Client Key"`
	ServerName string `help:"Used to set SNI host when connecting vial TLS"`
	Insecure   bool   `help:"Disable TLS hostname verification"`
	DC         string `help:"Consul Datacenter"`

	Version bool `short:"v" help:"Print version and exit"`
}

func makeClient(cmd *Cmd) (*api.Client, error) {
	config := api.DefaultConfig()

	if cmd.Address != "" {
		config.Address = cmd.Address
	}

	if cmd.Token != "" {
		config.Token = cmd.Token
	}

	var haveTls bool
	tls := api.TLSConfig{}
	if cmd.CaCert != "" {
		haveTls = true
		tls.CAFile = cmd.CaCert
	}

	if cmd.ClientCert != "" {
		haveTls = true
		tls.CertFile = cmd.ClientCert
	}

	if cmd.ClientKey != "" {
		haveTls = true
		tls.KeyFile = cmd.ClientKey
	}

	if cmd.Insecure {
		tls.InsecureSkipVerify = true
	}

	if haveTls {
		config.TLSConfig = tls
	}

	return api.NewClient(config)
}

func main() {
	runtime.GOMAXPROCS(1)

	cmd := new(Cmd)
	kong.Parse(cmd)

	if cmd.Version {
		fmt.Printf("consul-register %s (%s)\n", Version, runtime.Version())
		return
	}

	log := slog.New(slx.NewHandler(os.Stderr, slog.LevelDebug))
	ctx, cancel := context.WithCancel(slx.WithContext(context.Background(), log))

	client, err := makeClient(cmd)
	if err != nil {
		log.Error("failed to create consul client", "error", err)
		os.Exit(1)
	}

	config := &register.Config{
		Ctx: ctx,
		Sources: register.SourceConfig{
			ConfigDirectories: cmd.ConfigDir,
			ConfigFiles:       cmd.ConfigFile,
		},
		Client:                      client,
		DeletePreparedQueriesOnExit: cmd.DeletePreparedQueries,
	}

	svc, err := register.New(config)
	if err != nil {
		log.Error("could not start registration service", "error", err)
		os.Exit(1)
	}

	// signal handler channels
	quit := make(chan os.Signal, 1)
	reload := make(chan os.Signal, 1)

	// handle SIGINT,SIGTERM for quit and SIGHUP for reload
	signal.Notify(quit, syscall.SIGTERM, syscall.SIGINT)
	signal.Notify(reload, syscall.SIGHUP)

	// SIGTERM,SIGINT handler
	go func() {
		signal := <-quit
		if isatty.IsTerminal(os.Stderr.Fd()) {
			fmt.Fprintln(os.Stderr, signal)
		}
		cancel()
	}()

	// SIGHUP handler
	go func() {
		for {
			<-reload
			if err := svc.Reload(ctx); err != nil {
				log.Error("reload", "error", err)
			}
		}
	}()

	// start http server
	server, err := htx.NewContextServer(ctx, server.NewHandler(svc), "tcp", cmd.Listen)
	if err != nil {
		log.Error("could not start http server", "error", err)
		os.Exit(1)
	}

	// wait for exit
	<-ctx.Done()

	// wait for registration service shutdown
	if err := svc.Wait(time.Second * 5); err != nil {
		log.Error("graceful shutdown failed", "error", err)
		os.Exit(1)
	}

	// wait for http server shutdown
	if err := server.Wait(time.Second * 5); err != nil {
		log.Error("http server shutdown failed", "error", err)
		os.Exit(1)
	}

	log.Info("shutdown complete")
}
