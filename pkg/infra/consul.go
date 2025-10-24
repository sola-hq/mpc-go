package infra

import (
	"time"

	"github.com/fystack/mpcium/pkg/config"
	"github.com/fystack/mpcium/pkg/constant"
	"github.com/fystack/mpcium/pkg/logger"
	"github.com/hashicorp/consul/api"
)

type ConsulKV interface {
	Put(kv *api.KVPair, options *api.WriteOptions) (*api.WriteMeta, error)
	Get(key string, options *api.QueryOptions) (*api.KVPair, *api.QueryMeta, error)
	Delete(key string, options *api.WriteOptions) (*api.WriteMeta, error)
	List(prefix string, options *api.QueryOptions) (api.KVPairs, *api.QueryMeta, error)
}

func GetConsulConfig() *api.Config {
	cfg := config.GetConfig()
	environment := cfg.Environment
	var consulCfg *config.ConsulConfig
	if cfg.Consul != nil {
		consulCfg = cfg.Consul
	}

	config := api.DefaultConfig()
	if environment == constant.EnvProduction {
		config.Token = consulCfg.Token
		username := consulCfg.Username
		password := consulCfg.Password
		if username != "" || password != "" {
			config.HttpAuth = &api.HttpBasicAuth{
				Username: username,
				Password: password,
			}
		}
	}

	config.Address = consulCfg.Address
	if config.Address == "" {
		config.Address = api.DefaultConfig().Address
	}
	return config
}

func GetConsulClient(environment string) *api.Client {
	config := GetConsulConfig()
	config.WaitTime = 10 * time.Second

	logger.Info("Consul config",
		"environment", environment,
		"address", config.Address,
		"wait_time", config.WaitTime,
		"token_length", len(config.Token),
		"http_auth", config.HttpAuth,
	)

	// Ping the Consul server to verify connectivity
	client, err := api.NewClient(config)
	if err != nil {
		logger.Fatal("Failed to create consul client", err)
	}

	_, err = client.Status().Leader()
	if err != nil {
		logger.Fatal("failed to connect to Consul", err)
	}

	return client
}
