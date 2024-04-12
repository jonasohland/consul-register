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
	"github.com/r3labs/diff/v3"
	"github.com/samber/lo"
)

type QueryRegistration struct {
	def    *api.PreparedQueryDefinition
	client *api.Client

	deleteOnExit bool
	update       chan *api.PreparedQueryDefinition
	cancel       context.CancelFunc
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
	deleteOnExit bool,
	def *api.PreparedQueryDefinition,
) *QueryRegistration {
	ctx, cancel := context.WithCancel(slx.WithContext(ctx, slx.FromContext(ctx).With("query-name", def.Name)))

	registration := &QueryRegistration{
		def:    def,
		client: client,

		deleteOnExit: deleteOnExit,
		update:       make(chan *api.PreparedQueryDefinition),
		cancel:       cancel,
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

		queries, _, err := q.client.PreparedQuery().List((&api.QueryOptions{}).WithContext(ctx))
		if err != nil {
			return nil
		}

		existing, ok := lo.Find(queries, func(item *api.PreparedQueryDefinition) bool { return item.Name == q.def.Name })

		if ok {
			q.def.ID = existing.ID

			_, err := q.client.PreparedQuery().Update(q.def, (&api.WriteOptions{}).WithContext(ctx))
			if err != nil {
				log.Error("failed to update existing prepared query", "error", err)
				continue
			}

			log.Info("existing query updated", "query-id", q.def.ID)
		} else {
			// register the prepared query
			queryId, _, err := q.client.PreparedQuery().Create(q.def, (&api.WriteOptions{}).WithContext(ctx))
			if err != nil {
				log.Error("failed to establish prepared query", "error", err)
				continue
			}

			q.def.ID = queryId
			log.Info("prepared query established", "query-id", q.def.ID)
		}

		return nil
	}
}

func (q *QueryRegistration) keepRegistered(ctx context.Context) error {
	log := slx.FromContext(ctx)

	// establish the query initially
	if err := q.establish(ctx); err != nil {
		return err
	}

	// keep it established
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
		if err := q.renew(ctx); err != nil {
			// if the context is cancelled, return without trying to fix anything
			if ctx.Err() != nil {
				return nil
			}

			log.Error(
				"failed to renew query session",
				"query-id", q.def.ID,
				"error", err,
			)

			q.def.ID = ""

			// re-establish if an error occurs
			if err := q.establish(ctx); err != nil {
				log.Error("failed to re-establish service registration", "error", err)
			}
		}
	}
}

func (q *QueryRegistration) renew(ctx context.Context) error {
	def, _, err := q.client.PreparedQuery().Get(q.def.ID, (&api.QueryOptions{}).WithContext(ctx))
	if err != nil {
		return err
	}

	if len(def) != 0 {
		changes, err := diff.Diff(def[0], q.def)
		if err != nil {
			slx.FromContext(ctx).Error("failed to diff structs", "error", err)
		}

		if len(changes) > 0 {
			return fmt.Errorf("definition changed in consul: %s", ChangelogToString(changes))
		}

		return nil
	} else {
		return errors.New("prepared query was removed from consul")
	}
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

		_, err := q.client.PreparedQuery().Delete(q.def.ID, (&api.WriteOptions{}).WithContext(ctx))
		if err != nil {
			log.Error("failed to delete prepared query")
			continue
		}

		log.Info("prepared query deleted", "query-id", q.def.ID)
		q.def.ID = ""

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

	if err := q.keepRegistered(ctx); err != nil {
		log.Error("failed to keep query registration established")
	}

	if q.deleteOnExit {
		if err := q.destroy(log); err != nil {
			log.Error("failed to destroy prepared query")
		}
	}
}
