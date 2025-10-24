package messaging

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/fystack/mpcium/pkg/config"
	"github.com/fystack/mpcium/pkg/logger"
	"github.com/nats-io/nats.go"
)

const (
	// Default certificate paths
	defaultCertsDir   = "certs"
	defaultClientCert = "client-cert.pem"
	defaultClientKey  = "client-key.pem"
	defaultCACert     = "rootCA.pem"
)

// GetNATSConnection creates a NATS connection with proper TLS configuration
func GetNATSConnection() (*nats.Conn, error) {
	cfg := config.GetConfig()
	environment := cfg.Environment

	url := cfg.NATs.URL
	opts := []nats.Option{
		nats.MaxReconnects(-1), // retry forever
		nats.ReconnectWait(2 * time.Second),
		nats.DisconnectHandler(func(nc *nats.Conn) {
			logger.Warn("Disconnected from NATS")
		}),
		nats.ReconnectHandler(func(nc *nats.Conn) {
			logger.Info("Reconnected to NATS", "url", nc.ConnectedUrl())
		}),
		nats.ClosedHandler(func(nc *nats.Conn) {
			logger.Info("NATS connection closed!")
		}),
	}

	if environment == config.Production {
		tlsOpts, err := buildTLSOptions(cfg)
		if err != nil {
			return nil, err
		}
		opts = append(opts, tlsOpts...)
	}

	return nats.Connect(url, opts...)
}

// buildTLSOptions constructs TLS options for NATS connection
func buildTLSOptions(cfg *config.Config) ([]nats.Option, error) {
	certPaths := getCertificatePaths(cfg)

	// Validate certificate files exist
	if err := validateCertificateFiles(certPaths); err != nil {
		return nil, err
	}

	return []nats.Option{
		nats.ClientCert(certPaths.ClientCert, certPaths.ClientKey),
		nats.RootCAs(certPaths.CACert),
		nats.UserInfo(cfg.NATs.Username, cfg.NATs.Password),
	}, nil
}

// certificatePaths holds the paths to certificate files
type certificatePaths struct {
	ClientCert string
	ClientKey  string
	CACert     string
}

// getCertificatePaths returns certificate paths with fallback to defaults
func getCertificatePaths(cfg *config.Config) certificatePaths {
	paths := certificatePaths{}

	// Use configured paths if available
	if cfg.NATs.TLS != nil {
		paths.ClientCert = cfg.NATs.TLS.ClientCert
		paths.ClientKey = cfg.NATs.TLS.ClientKey
		paths.CACert = cfg.NATs.TLS.CACert
	}

	// Fallback to default paths if not configured
	if paths.ClientCert == "" {
		paths.ClientCert = filepath.Join(".", defaultCertsDir, defaultClientCert)
	}
	if paths.ClientKey == "" {
		paths.ClientKey = filepath.Join(".", defaultCertsDir, defaultClientKey)
	}
	if paths.CACert == "" {
		paths.CACert = filepath.Join(".", defaultCertsDir, defaultCACert)
	}

	return paths
}

// validateCertificateFiles checks if all required certificate files exist
func validateCertificateFiles(paths certificatePaths) error {
	requiredFiles := map[string]string{
		"client certificate": paths.ClientCert,
		"client key":         paths.ClientKey,
		"CA certificate":     paths.CACert,
	}

	for name, path := range requiredFiles {
		if _, err := os.Stat(path); os.IsNotExist(err) {
			return fmt.Errorf("%s not found at %s", name, path)
		}
	}

	return nil
}
