package register_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/hashicorp/consul/api"
	"github.com/jonasohland/consul-register/pkg/register"
)

func TestRegisterPreparedQuery(t *testing.T) {
	var wg sync.WaitGroup
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*20)
	defer cancel()

	client, _ := api.NewClient(api.DefaultConfig())

	def1 := &api.PreparedQueryDefinition{
		Name: "my-query",
		Service: api.ServiceQuery{
			Service:     "consul",
			OnlyPassing: true,
		},
	}

	def2 := &api.PreparedQueryDefinition{
		Name: "my-query",
		Service: api.ServiceQuery{
			Service:     "nomad",
			OnlyPassing: false,
		},
	}

	registration := register.NewPreparedQueryRegistration(ctx, &wg, client, true, def1)

	time.Sleep(5 * time.Second)

	registration.Update(def2)

	<-ctx.Done()

	wg.Wait()

}
