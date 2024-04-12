package register

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	config "github.com/hashicorp/consul/agent/config"

	"github.com/hashicorp/consul/agent/structs"
	"github.com/hashicorp/consul/api"
	mps "github.com/mitchellh/mapstructure"
	"github.com/samber/lo"
)

type SourceConfig struct {
	ConfigDirectories []string
	ConfigFiles       []string
}

type Config struct {
	Ctx context.Context

	Sources SourceConfig
	Client  *api.Client

	DeletePreparedQueriesOnExit bool
}

type ConfigAccumulator struct {
	Queries        []*api.PreparedQueryDefinition
	ConsulLoadOpts config.LoadOpts
}

type RuntimeConfig struct {
	Queries  []*api.PreparedQueryDefinition
	Services []*api.AgentServiceRegistration
}

func timeDurationToStringHookFunc() mps.DecodeHookFunc {
	return func(
		f reflect.Type,
		t reflect.Type,
		data interface{}) (interface{}, error) {
		dur, ok := data.(time.Duration)
		if !ok {
			return data, nil
		}
		if t.Kind() != reflect.String {
			return data, nil
		}
		if dur == 0 {
			return "", nil
		}

		// Convert it by parsing
		return data.(time.Duration).String(), nil
	}
}

func serviceToAgentService(svc *structs.ServiceDefinition) (*api.AgentServiceRegistration, error) {
	if svc.Name == "" {
		return nil, errors.New("service name is required")
	}

	var result api.AgentServiceRegistration
	d, err := mps.NewDecoder(&mps.DecoderConfig{
		Result:           &result,
		DecodeHook:       timeDurationToStringHookFunc(),
		WeaklyTypedInput: true,
	})
	if err != nil {
		return nil, err
	}
	if err := d.Decode(svc); err != nil {
		return nil, err
	}

	if result.Check != nil && reflect.DeepEqual(*result.Check, api.AgentServiceCheck{}) {
		result.Check = nil
	}

	if result.Proxy != nil && result.Proxy.TransparentProxy != nil && reflect.DeepEqual(*result.Proxy.TransparentProxy, api.TransparentProxyConfig{}) {
		result.Proxy.TransparentProxy = nil
	}
	if result.Proxy != nil && result.Proxy.AccessLogs != nil && reflect.DeepEqual(*result.Proxy.AccessLogs, api.AccessLogsConfig{}) {
		result.Proxy.AccessLogs = nil
	}

	return &result, nil
}

func loadFromFile(file string, into *ConfigAccumulator) error {
	if strings.HasSuffix(file, ".query.json") {
		fd, err := os.Open(file)
		if err != nil {
			return err
		}
		defer fd.Close()

		query := new(api.PreparedQueryDefinition)
		if err := json.NewDecoder(fd).Decode(query); err != nil {
			return err
		}

		into.Queries = append(into.Queries, query)
	} else if strings.HasSuffix(file, ".queries.json") {
		fd, err := os.Open(file)
		if err != nil {
			return err
		}
		defer fd.Close()

		queries := make([]*api.PreparedQueryDefinition, 0)
		if err := json.NewDecoder(fd).Decode(&queries); err != nil {
			return err
		}

		into.Queries = append(into.Queries, queries...)
	} else {
		into.ConsulLoadOpts.ConfigFiles = append(into.ConsulLoadOpts.ConfigFiles, file)
	}

	return nil
}

func loadFromDir(dir string, into *ConfigAccumulator) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		if err := loadFromFile(filepath.Join(dir, entry.Name()), into); err != nil {
			return err
		}
	}

	return nil
}

func LoadSources(sources *SourceConfig) (*RuntimeConfig, error) {
	accumulator := &ConfigAccumulator{}
	rtc := &RuntimeConfig{}

	for _, f := range sources.ConfigFiles {
		if err := loadFromFile(f, accumulator); err != nil {
			return nil, fmt.Errorf("failed to load configuration from '%s': %w", f, err)
		}
	}

	for _, d := range sources.ConfigDirectories {
		if err := loadFromDir(d, accumulator); err != nil {
			return nil, err
		}
	}

	rtc.Queries = accumulator.Queries
	accumulator.ConsulLoadOpts.DevMode = lo.ToPtr(true)

	result, err := config.Load(accumulator.ConsulLoadOpts)
	if err != nil {
		return nil, err
	}

	for _, service := range result.RuntimeConfig.Services {
		svc, err := serviceToAgentService(service)
		if err != nil {
			return nil, fmt.Errorf("failed to parse service definition files: %w", err)
		}

		rtc.Services = append(rtc.Services, svc)
	}

	return rtc, nil
}
