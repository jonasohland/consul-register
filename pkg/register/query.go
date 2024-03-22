package register

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"sync"
	"time"

	"github.com/hashicorp/consul/api"
	slx "github.com/jonasohland/slog-ext/pkg/slog-ext"
)

type QueryRegistration struct {
	def    *api.PreparedQueryDefinition
	client *api.Client

	update chan *api.PreparedQueryDefinition
	cancel context.CancelFunc
}

func (s *QueryRegistration) Update(def *api.PreparedQueryDefinition) {
	s.update <- def
}

func (s *QueryRegistration) Destroy() {
	s.cancel()
}

func NewPreparedQueryRegistration(
	ctx context.Context,
	wg *sync.WaitGroup,
	client *api.Client,
	def *api.PreparedQueryDefinition,
) *QueryRegistration {
	ctx, cancel := context.WithCancel(slx.WithContext(ctx, slx.FromContext(ctx).With("query-name", def.Name)))

	registration := &QueryRegistration{
		def:    def,
		client: client,

		update: make(chan *api.PreparedQueryDefinition),
		cancel: cancel,
	}

	wg.Add(1)
	go registration.run(ctx, wg)
	return registration
}

func (s *QueryRegistration) applyUpdate(def *api.PreparedQueryDefinition) bool {
	def.Session = s.def.Session
	def.ID = s.def.ID
	if !reflect.DeepEqual(def, s.def) {
		s.def = def
		return true
	}
	return false
}

func (q *QueryRegistration) establish(ctx context.Context) error {
	log := slx.FromContext(ctx)
	var wait time.Duration
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case def, ok := <-q.update:
			if !ok {
				return errors.New("update channel closed")
			}

			q.applyUpdate(def)
		case <-time.After(wait):
		}

		wait = Jitter(RetryRegisterTime, RetryRegisterJitter)

		if q.def.Session != "" || q.def.ID != "" {
			log.Info("destroy old query session", "session-id", q.def.Session)

			if err := q.retryDestroy(ctx); err != nil {
				log.Error("failed to destroy old prepared query registration")
				continue
			}
		}

		// create a session
		sessionId, _, err := q.client.Session().CreateNoChecks(&api.SessionEntry{
			LockDelay: time.Second * 5,
			Name:      fmt.Sprintf("prepared-query-%s", q.def.Name),
			Behavior:  "delete",
			TTL:       "10s",
		}, (&api.WriteOptions{}).WithContext(ctx))
		if err != nil {
			log.Error("failed to establish lock session", "error", err)
			continue
		}

		log.Info("session established", "session-id", sessionId)
		q.def.Session = sessionId

		// register the prepared query
		queryId, _, err := q.client.PreparedQuery().Create(q.def, (&api.WriteOptions{}).WithContext(ctx))
		if err != nil {
			log.Error("failed to establish prepared query", "error", err)
			continue
		}

		q.def.ID = queryId
		log.Info("prepared query established", "query-id", q.def.ID, "session-id", q.def.Session)
		return nil
	}
}

func (q *QueryRegistration) keep(ctx context.Context) error {
	log := slx.FromContext(ctx)
	wait := 7 * time.Second
	for {
		select {
		case <-ctx.Done():
			return nil
		case def, ok := <-q.update:
			if !ok {
				return errors.New("update channel closed")
			}

			if q.applyUpdate(def) {
				if err := q.establish(ctx); err != nil {
					log.Error("failed to re-establish prepared query registration", "error", err)
					continue
				}
			}
		case <-time.After(wait):
		}

		// check if the service is still registered
		if err := q.renew(ctx, &wait); err != nil {
			// if the context is cancelled, return without trying to fix anything
			if ctx.Err() != nil {
				return nil
			}

			log.Error(
				"failed to renew query session",
				"session-id", q.def.Session,
				"query-id", q.def.ID,
				"error", err,
			)

			// re-establish if an error occurs
			if err := q.establish(ctx); err != nil {
				log.Error("failed to re-establish service registration", "error", err)
			}
		}
	}
}

func (q *QueryRegistration) renew(ctx context.Context, wait *time.Duration) error {
	entry, _, err := q.client.Session().Renew(q.def.Session, (&api.WriteOptions{}).WithContext(ctx))
	if err != nil {
		return err
	}

	if entry == nil {
		return errors.New("session expired")
	}

	ttl, err := time.ParseDuration(entry.TTL)
	if err != nil {
		return fmt.Errorf("invalid ttl returned by consul: %w", err)
	}

	*wait = ttl / 2
	return nil
}

func (q *QueryRegistration) retryDestroy(ctx context.Context) error {
	log := slx.FromContext(ctx)
	var wait time.Duration
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(wait):
		}

		wait = Jitter(RetryRemoveTime, RetryRemoveJitter)

		if q.def.Session != "" {
			_, err := q.client.Session().Destroy(q.def.ID, (&api.WriteOptions{}).WithContext(ctx))
			if err != nil {
				log.Error("failed to destroy query session")
				continue
			}

			log.Info("query session destroyed", "session-id", q.def.Session)
			q.def.Session = ""
		}

		if q.def.ID != "" {
			_, err := q.client.PreparedQuery().Delete(q.def.ID, (&api.WriteOptions{}).WithContext(ctx))
			if err != nil {
				log.Error("failed to delete prepared query")
				continue
			}

			log.Info("prepared query deleted", "query-id", q.def.ID)
			q.def.ID = ""
		}

		return nil
	}
}

func (q *QueryRegistration) destroy(log *slog.Logger) error {
	ctx, cancel := context.WithTimeout(slx.WithContext(context.Background(), log), time.Second*5)
	defer cancel()

	return q.retryDestroy(ctx)
}

func (q *QueryRegistration) run(ctx context.Context, wg *sync.WaitGroup) {
	defer wg.Done()
	log := slx.FromContext(ctx)

	if err := q.establish(ctx); err != nil {
		log.Error("failed to establish service registration")
		return
	}

	if err := q.keep(ctx); err != nil {
		log.Error("failed to keep query registration established")
	}

	if err := q.destroy(log); err != nil {
		log.Error("failed to destroy prepared query")
	}
}
