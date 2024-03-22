package register

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/hashicorp/consul/api"
	slx "github.com/jonasohland/slog-ext/pkg/slog-ext"
)

const (
	RetryRegisterTime   = time.Second * 1
	RetryRegisterJitter = time.Second * 3

	RetryRemoveTime   = time.Second * 1
	RetryRemoveJitter = time.Second * 1

	CheckInterval = time.Second * 7
	CheckJitter   = time.Second * 3
)

func NewServiceRegistration(
	ctx context.Context,
	wg *sync.WaitGroup,
	client *api.Client,
	def *api.AgentServiceRegistration,
) *ServiceRegistration {
	if def.ID == "" {
		def.ID = fmt.Sprintf("%s-%s", def.Name, uuid.NewString())
	}
	ctx, cancel := context.WithCancel(slx.WithContext(ctx, slx.FromContext(ctx).With("service", def.Name, "id", def.ID)))
	registration := &ServiceRegistration{
		def:    def,
		client: client,
		update: make(chan *api.AgentServiceRegistration),
		cancel: cancel,
	}

	wg.Add(1)
	go registration.run(ctx, wg)
	return registration
}

type ServiceRegistration struct {
	def    *api.AgentServiceRegistration
	client *api.Client

	update chan *api.AgentServiceRegistration
	cancel context.CancelFunc
}

func (s *ServiceRegistration) Update(reg *api.AgentServiceRegistration) {
	s.update <- reg
}

func (s *ServiceRegistration) Destroy() {
	s.cancel()
}

func (r *ServiceRegistration) applyUpdate(def *api.AgentServiceRegistration) bool {
	def.ID = r.def.ID
	if !reflect.DeepEqual(r.def, def) {
		r.def = def
		return true
	}

	return false
}

func (r *ServiceRegistration) establish(ctx context.Context) error {
	var wait time.Duration
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case def, ok := <-r.update:
			if !ok {
				return errors.New("update channel closed")
			}

			r.applyUpdate(def)
		case <-time.After(wait):
		}

		wait = Jitter(RetryRegisterTime, RetryRegisterJitter)

		err := r.client.Agent().ServiceRegisterOpts(r.def, (&api.ServiceRegisterOpts{}).WithContext(ctx))
		if err != nil {
			slx.FromContext(ctx).Error("failed to establish service", "error", err)
			continue
		}

		slx.FromContext(ctx).Info("service established")
		return nil
	}
}

func (r *ServiceRegistration) keep(ctx context.Context) error {
	log := slx.FromContext(ctx)
	for {
		select {
		case <-ctx.Done():
			return nil
		case def, ok := <-r.update:
			if !ok {
				return errors.New("update channel closed")
			}

			if r.applyUpdate(def) {
				if err := r.establish(ctx); err != nil {
					log.Error("failed to re-establish service registration", "error", err)
					continue
				}
				log.Info("service updated")
			}
		case <-time.After(10 * time.Second):
		}

		// check if the service is still registered
		if err := r.check(ctx); err != nil {
			// if the context is cancelled, return without trying to fix anything
			if ctx.Err() != nil {
				return nil
			}

			log.Error("failed to check for active service registration", "error", err)

			// re-establish if an error occurs
			if err := r.establish(ctx); err != nil {
				log.Error("failed to re-establish service registration", "error", err)
			}
		}
	}
}

func (r *ServiceRegistration) check(ctx context.Context) error {
	log := slx.FromContext(ctx)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case def, ok := <-r.update:
			if !ok {
				return errors.New("update channel closed")
			}

			if r.applyUpdate(def) {
				if err := r.establish(ctx); err != nil {
					log.Error("failed to re-establish service registration", "error", err)
					continue
				}
				log.Info("service updated")
			}
		case <-time.After(Jitter(CheckInterval, CheckJitter)):
		}

		_, _, err := r.client.Agent().Service(r.def.ID, (&api.QueryOptions{}).WithContext(ctx))
		if err != nil {
			return err
		}
	}
}

func (r *ServiceRegistration) removeRetry(ctx context.Context) error {
	log := slx.FromContext(ctx)
	var wait time.Duration
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(wait):
		}

		wait = Jitter(RetryRemoveTime, RetryRemoveJitter)

		if err := r.client.Agent().ServiceDeregisterOpts(r.def.ID, (&api.QueryOptions{}).WithContext(ctx)); err != nil {
			log.Error("failed to deregister service", "error", err)
			continue
		}

		log.Info("service removed")
		return nil
	}
}

func (r *ServiceRegistration) remove(log *slog.Logger) error {
	ctx, cancel := context.WithTimeout(slx.WithContext(context.Background(), log), time.Second*5)
	defer cancel()

	return r.removeRetry(ctx)
}

func (r *ServiceRegistration) run(ctx context.Context, wg *sync.WaitGroup) {
	defer wg.Done()
	log := slx.FromContext(ctx)

	if err := r.establish(ctx); err != nil {
		log.Error("failed to establish service registration", "error", err)
		return
	}

	if err := r.keep(ctx); err != nil {
		log.Error("failed to keep the service established", "error", err)
		return
	}

	if err := r.remove(log); err != nil {
		log.Error("failed to remove service registration", "error", err)
	}
}
