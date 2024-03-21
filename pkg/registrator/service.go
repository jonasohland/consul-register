package registrator

import (
	"context"
	"sync"
	"time"

	"github.com/hashicorp/consul/api"
	slx "github.com/jonasohland/slog-ext/pkg/slog-ext"
)

func NewServiceRegistration(definition *api.AgentServiceRegistration, client *api.Client) *ServiceRegistration {
	return &ServiceRegistration{
		def:    definition,
		client: client,
	}
}

type ServiceRegistration struct {
	def    *api.AgentServiceRegistration
	client *api.Client
}

func (r *ServiceRegistration) establish(ctx context.Context) error {
	return nil
}

func (r *ServiceRegistration) keep(ctx context.Context) error {
	log := slx.FromContext(ctx)
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(10 * time.Second):
		}

		// check if the service is still registered
		if err := r.check(ctx); err != nil {

			log.Error("failed to check for active service registration", "error", err)

			// if the context is cancelled, return without trying to fix anything
			if ctx.Err() != nil {
				return nil
			}

			// re-establish if an error occurs
			if err := r.establish(ctx); err != nil {
				log.Error("failed to re-establish service registration", "error", err)
			}
		}
	}
}

func (r *ServiceRegistration) check(ctx context.Context) error {
	return nil
}

func (r *ServiceRegistration) remove() error {
	return nil
}

func (r *ServiceRegistration) Run(ctx context.Context, wg *sync.WaitGroup) {
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

	if err := r.remove(); err != nil {
		log.Error("failed to remove service registration", "error", err)
	}
}
