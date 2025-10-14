package messaging

import (
	"path/filepath"
	"time"

	"github.com/fystack/mpcium/pkg/config"
	"github.com/fystack/mpcium/pkg/constant"
	"github.com/fystack/mpcium/pkg/logger"
	"github.com/nats-io/nats.go"
)

// GetNATSConnection creates a NATS connection with proper TLS configuration
func GetNATSConnection(environment string, config *config.NATsConfig) (*nats.Conn, error) {
	url := config.URL
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

	if environment == constant.EnvProduction {
		// Load TLS config from configuration
		var clientCert, clientKey, caCert string
		if config.TLS != nil {
			clientCert = config.TLS.ClientCert
			clientKey = config.TLS.ClientKey
			caCert = config.TLS.CACert
		}

		// Fallback to default paths if not configured
		if clientCert == "" {
			clientCert = filepath.Join(".", "certs", "client-cert.pem")
		}
		if clientKey == "" {
			clientKey = filepath.Join(".", "certs", "client-key.pem")
		}
		if caCert == "" {
			caCert = filepath.Join(".", "certs", "rootCA.pem")
		}

		opts = append(opts,
			nats.ClientCert(clientCert, clientKey),
			nats.RootCAs(caCert),
			nats.UserInfo(config.Username, config.Password),
		)
	}

	return nats.Connect(url, opts...)
}
