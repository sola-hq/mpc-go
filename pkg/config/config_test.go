package config

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestConsulConfig(t *testing.T) {
	config := ConsulConfig{
		Address:  "consul.example.com:8500",
		Username: "consul_user",
		Password: "consul_pass",
		Token:    "consul_token",
	}

	assert.Equal(t, "consul.example.com:8500", config.Address)
	assert.Equal(t, "consul_user", config.Username)
	assert.Equal(t, "consul_pass", config.Password)
	assert.Equal(t, "consul_token", config.Token)
}

func TestNATsConfig(t *testing.T) {
	config := NATsConfig{
		URL:      "nats://nats.example.com:4222",
		Username: "nats_user",
		Password: "nats_pass",
	}

	assert.Equal(t, "nats://nats.example.com:4222", config.URL)
	assert.Equal(t, "nats_user", config.Username)
	assert.Equal(t, "nats_pass", config.Password)
}

func TestConfig_ApplyDefaults(t *testing.T) {
	config := &Config{}
	applyDefaults(config)

	assert.Equal(t, Development, config.Environment)
	assert.Equal(t, defaultBadgerDBPath, config.DBPath)
	assert.Equal(t, defaultBackupDir, config.BackupDir)
	assert.Equal(t, defaultBackupPeriodSeconds, config.BackupPeriodSeconds)
	assert.Equal(t, defaultMaxConcurrentKeygen, config.MaxConcurrentKeygen)
	assert.Equal(t, defaultMaxConcurrentSigning, config.MaxConcurrentSigning)
	assert.Equal(t, defaultMaxConcurrentResharing, config.MaxConcurrentResharing)
	assert.Equal(t, defaultSessionWarmUpDelayMs, config.SessionWarmUpDelayMillis)
	assert.Equal(t, defaultThreshold, config.Threshold)
	assert.Equal(t, defaultInitiatorAlgorithm, config.EventInitiatorAlgorithm)
}

func TestConfig_ApplyDefaults_WithExistingValues(t *testing.T) {
	config := &Config{
		Environment: "production",
		DBPath:      "/custom/path",
		Threshold:   3,
	}
	applyDefaults(config)

	// Should not override existing values
	assert.Equal(t, "production", config.Environment)
	assert.Equal(t, "/custom/path", config.DBPath)
	assert.Equal(t, 3, config.Threshold)

	// Should apply defaults for empty values
	assert.Equal(t, defaultBackupDir, config.BackupDir)
	assert.Equal(t, defaultMaxConcurrentKeygen, config.MaxConcurrentKeygen)
}

func TestValidateEnvironment(t *testing.T) {
	tests := []struct {
		name        string
		environment string
		wantErr     bool
	}{
		{
			name:        "valid production environment",
			environment: "production",
			wantErr:     false,
		},
		{
			name:        "valid development environment",
			environment: "development",
			wantErr:     false,
		},
		{
			name:        "invalid environment",
			environment: "staging",
			wantErr:     true,
		},
		{
			name:        "empty environment",
			environment: "",
			wantErr:     true,
		},
		{
			name:        "case sensitive - Production",
			environment: "Production",
			wantErr:     true,
		},
		{
			name:        "case sensitive - PRODUCTION",
			environment: "PRODUCTION",
			wantErr:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateEnvironment(tt.environment)
			if tt.wantErr {
				assert.Error(t, err)
				if err != nil {
					assert.Contains(t, err.Error(), "invalid environment")
					assert.Contains(t, err.Error(), "production, development")
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestConfigAccessFunctions(t *testing.T) {
	// Create a test config
	testConfig := &Config{
		BadgerPassword:           "test_password",
		EventInitiatorAlgorithm:  "ed25519",
		EventInitiatorPubKey:     "test_pubkey",
		Threshold:                2,
		MaxConcurrentKeygen:      3,
		MaxConcurrentSigning:     5,
		MaxConcurrentResharing:   2,
		SessionWarmUpDelayMillis: 200,
		DBPath:                   "/test/db",
		BackupEnabled:            true,
		BackupPeriodSeconds:      600,
		BackupDir:                "/test/backup",
	}

	// Set the global config
	setConfig(testConfig)

	// Test all access functions
	assert.Equal(t, "test_password", BadgerPassword())
	assert.Equal(t, "ed25519", EventInitiatorAlgorithm())
	assert.Equal(t, "test_pubkey", EventInitiatorPubKey())
	assert.Equal(t, 2, Threshold())
	assert.Equal(t, 3, MaxConcurrentKeygen())
	assert.Equal(t, 5, MaxConcurrentSigning())
	assert.Equal(t, 2, MaxConcurrentResharing())
	assert.Equal(t, 200, SessionWarmUpDelayMillis())
	assert.Equal(t, "/test/db", DBPath())
	assert.Equal(t, true, BackupEnabled())
	assert.Equal(t, 600, BackupPeriodSeconds())
	assert.Equal(t, "/test/backup", BackupDir())
}

func TestSetBadgerPassword(t *testing.T) {
	// Create a test config
	testConfig := &Config{
		BadgerPassword: "original_password",
	}
	setConfig(testConfig)

	// Test setting password
	SetBadgerPassword("new_password")
	assert.Equal(t, "new_password", BadgerPassword())
}

func TestGetConfig_FatalWhenNotLoaded(t *testing.T) {
	// This test is skipped because GetConfig() calls logger.Fatal()
	// which calls os.Exit(1) and cannot be tested in a unit test
	// In practice, this function should never be called before Load()
	t.Skip("GetConfig() calls logger.Fatal() which cannot be tested in unit tests")
}

func TestUpdate_PanicWhenNotLoaded(t *testing.T) {
	// Reset global config to nil
	mu.Lock()
	originalApp := app
	app = nil
	mu.Unlock()

	// Restore after test
	defer func() {
		mu.Lock()
		app = originalApp
		mu.Unlock()
	}()

	// This should panic
	assert.Panics(t, func() {
		Update(func(cfg *Config) {
			cfg.Threshold = 5
		})
	})
}

func TestSetEnvConfigPath(t *testing.T) {
	// Test setting config path
	SetEnvConfigPath("/test/config.yaml")
	assert.Equal(t, "/test/config.yaml", os.Getenv(EnvConfigFile))

	// Test setting empty path (should not change env)
	SetEnvConfigPath("")
	assert.Equal(t, "/test/config.yaml", os.Getenv(EnvConfigFile))

	// Clean up
	os.Unsetenv(EnvConfigFile)
}

func TestTLSConfig(t *testing.T) {
	config := TLSConfig{
		ClientCert: "client.crt",
		ClientKey:  "client.key",
		CACert:     "ca.crt",
	}

	assert.Equal(t, "client.crt", config.ClientCert)
	assert.Equal(t, "client.key", config.ClientKey)
	assert.Equal(t, "ca.crt", config.CACert)
}

func TestNATsConfig_WithTLS(t *testing.T) {
	tlsConfig := &TLSConfig{
		ClientCert: "client.crt",
		ClientKey:  "client.key",
		CACert:     "ca.crt",
	}

	config := NATsConfig{
		URL:      "nats://secure.example.com:4222",
		Username: "nats_user",
		Password: "nats_pass",
		TLS:      tlsConfig,
	}

	assert.Equal(t, "nats://secure.example.com:4222", config.URL)
	assert.Equal(t, "nats_user", config.Username)
	assert.Equal(t, "nats_pass", config.Password)
	assert.Equal(t, tlsConfig, config.TLS)
	assert.Equal(t, "client.crt", config.TLS.ClientCert)
}
