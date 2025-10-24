package infra

import (
	"time"

	"github.com/fystack/mpcium/pkg/config"
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

	clientConfig := api.DefaultConfig()
	if environment == config.Production {
		clientConfig.Token = consulCfg.Token
		username := consulCfg.Username
		password := consulCfg.Password
		if username != "" || password != "" {
			clientConfig.HttpAuth = &api.HttpBasicAuth{
				Username: username,
				Password: password,
			}
		}
	}

	clientConfig.Address = consulCfg.Address
	if clientConfig.Address == "" {
		clientConfig.Address = api.DefaultConfig().Address
	}
	return clientConfig
}

func GetConsulClient(environment string) *api.Client {
	cfg := GetConsulConfig()
	cfg.WaitTime = 10 * time.Second

	logger.Info("Consul config",
		"environment", environment,
		"address", cfg.Address,
		"wait_time", cfg.WaitTime,
		"token_length", len(cfg.Token),
		"http_auth", cfg.HttpAuth,
	)

	// Ping the Consul server to verify connectivity
	client, err := api.NewClient(cfg)
	if err != nil {
		logger.Fatal("Failed to create consul client", err)
	}

	_, err = client.Status().Leader()
	if err != nil {
		logger.Fatal("failed to connect to Consul", err)
	}

	return client
}
