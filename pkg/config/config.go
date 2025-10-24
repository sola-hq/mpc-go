package config

import (
	"fmt"
	"os"
	"slices"
	"strings"
	"sync"

	"github.com/fystack/mpcium/pkg/logger"
	"github.com/go-viper/mapstructure/v2"
	"github.com/spf13/viper"
)

const (
	// Environment constants
	Production  = "production"
	Development = "development"

	defaultStorageType            = "badger"
	defaultBadgerDBPath           = "."
	defaultBackupDir              = "backups"
	defaultBackupPeriodSeconds    = 300
	defaultMaxConcurrentKeygen    = 2
	defaultMaxConcurrentSigning   = 10
	defaultMaxConcurrentResharing = 5
	defaultSessionWarmUpDelayMs   = 10
	defaultThreshold              = 1
	defaultInitiatorAlgorithm     = "ed25519"

	EnvConfigFile = "MPC_CONFIG_FILE"
)

type Config struct {
	Consul *ConsulConfig `mapstructure:"consul"`
	NATs   *NATsConfig   `mapstructure:"nats"`

	Environment string `mapstructure:"environment"`

	// Storage configuration
	BadgerPassword string `mapstructure:"badger_password"`
	DBPath         string `mapstructure:"db_path"`

	BackupDir           string `mapstructure:"backup_dir"`
	BackupEnabled       bool   `mapstructure:"backup_enabled"`
	BackupPeriodSeconds int    `mapstructure:"backup_period_seconds"`

	EventInitiatorAlgorithm string `mapstructure:"event_initiator_algorithm"`
	EventInitiatorPubKey    string `mapstructure:"event_initiator_pubkey"`

	Threshold                int `mapstructure:"threshold"`
	MaxConcurrentKeygen      int `mapstructure:"max_concurrent_keygen"`
	MaxConcurrentSigning     int `mapstructure:"max_concurrent_signing"`
	MaxConcurrentResharing   int `mapstructure:"max_concurrent_resharing"`
	SessionWarmUpDelayMillis int `mapstructure:"session_warm_up_delay_ms"`
}

type ConsulConfig struct {
	Address  string `mapstructure:"address"`
	Username string `mapstructure:"username"`
	Password string `mapstructure:"password"`
	Token    string `mapstructure:"token"`
}

type NATsConfig struct {
	URL      string     `mapstructure:"url"`
	Username string     `mapstructure:"username"`
	Password string     `mapstructure:"password"`
	TLS      *TLSConfig `mapstructure:"tls"`
}

type TLSConfig struct {
	ClientCert string `mapstructure:"client_cert"`
	ClientKey  string `mapstructure:"client_key"`
	CACert     string `mapstructure:"ca_cert"`
}

var (
	app *Config
	mu  sync.RWMutex
)

func initConfig() error {
	// env
	viper.SetEnvPrefix("MPC")
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	viper.AutomaticEnv()

	viper.SetDefault("environment", Development)
	viper.SetDefault("storage_type", defaultStorageType)
	viper.SetDefault("db_path", defaultBadgerDBPath)
	viper.SetDefault("backup_dir", defaultBackupDir)
	viper.SetDefault("backup_period_seconds", defaultBackupPeriodSeconds)
	viper.SetDefault("backup_enabled", true)
	viper.SetDefault("threshold", defaultThreshold)
	viper.SetDefault("event_initiator_algorithm", defaultInitiatorAlgorithm)
	viper.SetDefault("max_concurrent_keygen", defaultMaxConcurrentKeygen)
	viper.SetDefault("max_concurrent_signing", defaultMaxConcurrentSigning)
	viper.SetDefault("max_concurrent_resharing", defaultMaxConcurrentResharing)
	viper.SetDefault("session_warm_up_delay_ms", defaultSessionWarmUpDelayMs)

	// set env config file
	configFile := os.Getenv(EnvConfigFile)
	if configFile != "" {
		viper.SetConfigFile(configFile)
	} else {
		viper.SetConfigName("config")
		viper.SetConfigType("yaml")
		viper.AddConfigPath(".")
		viper.AddConfigPath("/etc/mpc/")
		viper.AddConfigPath("$HOME/.mpc/")
	}

	if err := viper.ReadInConfig(); err != nil {
		return fmt.Errorf("viper read config: %w", err)
	}

	return nil
}

func SetEnvConfigPath(configPath string) {
	if configPath != "" {
		os.Setenv(EnvConfigFile, configPath)
	}
}

func LoadConfig() (*Config, error) {
	var cfg Config
	decoderConfig := &mapstructure.DecoderConfig{
		Result:           &cfg,
		WeaklyTypedInput: true,
		DecodeHook: mapstructure.ComposeDecodeHookFunc(
			mapstructure.StringToTimeDurationHookFunc(),
			mapstructure.StringToSliceHookFunc(","),
		),
	}

	decoder, err := mapstructure.NewDecoder(decoderConfig)
	if err != nil {
		return nil, fmt.Errorf("create decoder: %w", err)
	}

	if err := decoder.Decode(viper.AllSettings()); err != nil {
		return nil, fmt.Errorf("decode config: %w", err)
	}

	applyDefaults(&cfg)

	if err := validateEnvironment(cfg.Environment); err != nil {
		return nil, fmt.Errorf("config validation failed: %w", err)
	}

	setConfig(&cfg)
	return &cfg, nil
}

func Load() (*Config, error) {
	if err := initConfig(); err != nil {
		return nil, err
	}
	return LoadConfig()
}

func validateEnvironment(environment string) error {
	validEnvironments := []string{Production, Development}

	if !slices.Contains(validEnvironments, environment) {
		return fmt.Errorf("invalid environment '%s'. Must be one of: %s", environment, strings.Join(validEnvironments, ", "))
	}
	return nil
}

func applyDefaults(cfg *Config) {
	if cfg.Environment == "" {
		cfg.Environment = Development
	}
	if cfg.DBPath == "" {
		cfg.DBPath = defaultBadgerDBPath
	}
	if cfg.BackupDir == "" {
		cfg.BackupDir = defaultBackupDir
	}
	if cfg.BackupPeriodSeconds == 0 {
		cfg.BackupPeriodSeconds = defaultBackupPeriodSeconds
	}
	if cfg.MaxConcurrentKeygen == 0 {
		cfg.MaxConcurrentKeygen = defaultMaxConcurrentKeygen
	}
	if cfg.MaxConcurrentSigning == 0 {
		cfg.MaxConcurrentSigning = defaultMaxConcurrentSigning
	}
	if cfg.MaxConcurrentResharing == 0 {
		cfg.MaxConcurrentResharing = defaultMaxConcurrentResharing
	}
	if cfg.SessionWarmUpDelayMillis == 0 {
		cfg.SessionWarmUpDelayMillis = defaultSessionWarmUpDelayMs
	}
	if cfg.Threshold == 0 {
		cfg.Threshold = defaultThreshold
	}
	if cfg.EventInitiatorAlgorithm == "" {
		cfg.EventInitiatorAlgorithm = defaultInitiatorAlgorithm
	}
}

func setConfig(cfg *Config) {
	mu.Lock()
	defer mu.Unlock()
	app = cfg
}

// These helper functions centralize access to runtime configuration data.

// Current returns the in-memory application configuration.
// It panics if the configuration has not been loaded yet.
func GetConfig() *Config {
	mu.RLock()
	defer mu.RUnlock()
	if app == nil {
		logger.Fatal("configuration not loaded", nil)
	}
	return app
}

// Update applies the provided function while holding the configuration write lock.
// It panics if the configuration has not been loaded yet.
func Update(fn func(cfg *Config)) {
	mu.Lock()
	defer mu.Unlock()
	if app == nil {
		panic("configuration not loaded")
	}
	fn(app)
}

func BadgerPassword() string {
	return GetConfig().BadgerPassword
}

func SetBadgerPassword(password string) {
	Update(func(cfg *Config) {
		cfg.BadgerPassword = password
	})
}

func EventInitiatorAlgorithm() string {
	return GetConfig().EventInitiatorAlgorithm
}

func EventInitiatorPubKey() string {
	return GetConfig().EventInitiatorPubKey
}

func Threshold() int {
	return GetConfig().Threshold
}

func MaxConcurrentKeygen() int {
	return GetConfig().MaxConcurrentKeygen
}

func MaxConcurrentSigning() int {
	return GetConfig().MaxConcurrentSigning
}

func MaxConcurrentResharing() int {
	return GetConfig().MaxConcurrentResharing
}

func SessionWarmUpDelayMillis() int {
	return GetConfig().SessionWarmUpDelayMillis
}

func DBPath() string {
	return GetConfig().DBPath
}

func BackupEnabled() bool {
	return GetConfig().BackupEnabled
}

func BackupPeriodSeconds() int {
	return GetConfig().BackupPeriodSeconds
}

func NATs() *NATsConfig {
	return GetConfig().NATs
}

func Environment() string {
	return GetConfig().Environment
}

func BackupDir() string {
	return GetConfig().BackupDir
}

func IsProduction() bool {
	return strings.EqualFold(Environment(), Production)
}
