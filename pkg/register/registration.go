package register

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/hashicorp/consul/api"
	errx "github.com/jonasohland/ext/errors"
	syncx "github.com/jonasohland/ext/sync"
	slx "github.com/jonasohland/slog-ext/pkg/slog-ext"
	"github.com/samber/lo"
)

const (
	ConnectionMonitoringInterval = 10 * time.Second
	ConnectionMonitoringJitter   = 5 * time.Second
)

type RegistrationService interface {
	Health() error
	Reload(ctx context.Context) error
	Wait(timeout time.Duration) error
}

type registrationSvc struct {
	log    *slog.Logger
	client *api.Client

	reloadCh chan<- chan error
	sources  SourceConfig

	services map[string]*ServiceRegistration
	queries  map[string]*QueryRegistration

	wg        sync.WaitGroup
	lastError errx.LastError
}

func (r *registrationSvc) Health() error {
	if err := r.lastError.Get(); err != nil {
		return err
	}

	return nil
}

func (r *registrationSvc) Wait(timeout time.Duration) error {
	return syncx.WaitTimeout(&r.wg, timeout)
}

func (r *registrationSvc) Reload(ctx context.Context) error {
	responder := make(chan error)
	select {
	case r.reloadCh <- responder:
	case <-ctx.Done():
		return ctx.Err()
	}

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

func (r *registrationSvc) runConnectionMonitoring(ctx context.Context) {
	defer r.wg.Done()

	var wait time.Duration
	for {
		select {
		case <-time.After(wait):
		case <-ctx.Done():
			return
		}

		wait = Jitter(ConnectionMonitoringInterval, ConnectionMonitoringJitter)

		_, err := r.client.Status().Leader()
		if err != nil {
			last := r.lastError.Set(err)
			if last == nil {
				r.log.Warn("consul connection unhealthy", "error", err)
			}
		} else {
			if err := r.lastError.Clear(); err != nil {
				r.log.Info("consul connection")
			}
		}
	}
}

func (r *registrationSvc) runReloader(ctx context.Context, ch <-chan chan error) {
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

func (r *registrationSvc) reload(ctx context.Context) error {
	r.log.Info("configuration reload triggered")
	rtc, err := LoadSources(&r.sources)
	if err != nil {
		return err
	}

	// Add/Update existing service registrations
	for _, def := range rtc.Services {
		existing, ok := r.services[def.Name]
		if ok {
			existing.Update(def)
			continue
		}

		r.services[def.Name] = NewServiceRegistration(ctx, &r.wg, r.client, def)
	}

	// Add/Update existing prepared query registrations
	for _, def := range rtc.Queries {
		existing, ok := r.queries[def.Name]
		if ok {
			existing.Update(def)
			continue
		}

		r.queries[def.Name] = NewPreparedQueryRegistration(ctx, &r.wg, r.client, def)
	}

	// Remove no longer references service registrations
	for _, name := range lo.Keys(r.services) {
		if !lo.ContainsBy(rtc.Services, func(item *api.AgentServiceRegistration) bool { return item.Name == name }) {
			r.services[name].Destroy()
			delete(r.services, name)
		}
	}

	// Remove no longer references prepared queries
	for _, name := range lo.Keys(r.queries) {
		if !lo.ContainsBy(rtc.Queries, func(item *api.PreparedQueryDefinition) bool { return item.Name == name }) {
			r.queries[name].Destroy()
			delete(r.queries, name)
		}
	}

	return nil
}

func New(config *Config) (RegistrationService, error) {
	if config.Ctx == nil {
		config.Ctx = context.Background()
	}

	if config.Client == nil {
		client, err := api.NewClient(api.DefaultConfig())
		if err != nil {
			panic(err)
		}

		config.Client = client
	}

	log := slx.FromContext(config.Ctx)

	reloadCh := make(chan chan error)
	r := &registrationSvc{
		log:    log,
		client: config.Client,

		reloadCh: reloadCh,
		sources:  config.Sources,

		services: make(map[string]*ServiceRegistration),
		queries:  make(map[string]*QueryRegistration),
	}

	r.wg.Add(2)
	go r.runConnectionMonitoring(config.Ctx)
	go r.runReloader(config.Ctx, reloadCh)

	if err := r.Reload(config.Ctx); err != nil {
		return nil, err
	}

	return r, nil
}
