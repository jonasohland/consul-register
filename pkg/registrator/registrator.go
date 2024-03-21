package registrator

import (
	"context"
	"log/slog"
	"sync"
	"time"

	errx "github.com/jonasohland/ext/errors"
	syncx "github.com/jonasohland/ext/sync"
	slx "github.com/jonasohland/slog-ext/pkg/slog-ext"
	"github.com/kr/pretty"
)

type Registrator interface {
	Health() error
	Reload(ctx context.Context) error
	Wait(timeout time.Duration) error
}

type registrator struct {
	log *slog.Logger

	reloadCh chan<- chan error
	sources  SourceConfig

	wg        sync.WaitGroup
	lastError errx.LastError
}

func (r *registrator) Health() error {
	if err := r.lastError.Get(); err != nil {
		return err
	}

	return nil
}

func (r *registrator) Wait(timeout time.Duration) error {
	return syncx.WaitTimeout(&r.wg, timeout)
}

func (r *registrator) Reload(ctx context.Context) error {
	responder := make(chan error)
	select {
	case r.reloadCh <- responder:
	case <-ctx.Done():
		return ctx.Err()
	}

	r.log.Info("trigger reload")
	select {
	case err, ok := <-responder:
		if !ok {
			return nil
		}

		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (r *registrator) runConnectionMonitoring(ctx context.Context) {
	defer r.wg.Done()
}

func (r *registrator) runReloader(ctx context.Context, ch <-chan chan error) {
	defer r.wg.Done()
	for {
		select {
		case <-ctx.Done():
			return
		case responder, ok := <-ch:
			if !ok {
				return
			}
			if err := r.reload(ctx); err != nil {
				responder <- err
			}
			close(responder)
		}
	}
}

func (r *registrator) reload(ctx context.Context) error {
	rtc, err := LoadSources(&r.sources)
	if err != nil {
		return err
	}

	pretty.Println(rtc)

	return nil
}

func NewRegistrator(config *Config) (Registrator, error) {
	if config.Ctx == nil {
		config.Ctx = context.Background()
	}

	log := slx.FromContext(config.Ctx)

	reloadCh := make(chan chan error)
	r := &registrator{
		log:      log,
		reloadCh: reloadCh,
		sources:  config.Sources,
	}

	r.wg.Add(2)
	go r.runConnectionMonitoring(config.Ctx)
	go r.runReloader(config.Ctx, reloadCh)
	return r, nil
}
