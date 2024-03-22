package register_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/hashicorp/consul/api"
	"github.com/jonasohland/consul-register/pkg/register"
)

func TestRegisterService(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*21)
	defer cancel()

	client, err := api.NewClient(api.DefaultConfig())
	if err != nil {
		panic(err)
	}
	wg := sync.WaitGroup{}

	reg1 := &api.AgentServiceRegistration{
		Name: "my-service",
		Port: 1234,
	}

	reg2 := &api.AgentServiceRegistration{
		Name: "my-service",
		Port: 1234,
	}

	registration := register.NewServiceRegistration(ctx, &wg, client, reg1)
	time.Sleep(time.Second * 5)
	registration.Update(reg2)

	<-ctx.Done()
	wg.Wait()
}
