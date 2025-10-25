package storage

import (
	"fmt"
	"path/filepath"

	"github.com/fystack/mpcium/pkg/config"
	"github.com/fystack/mpcium/pkg/logger"
)

func NewKVStore(nodeName, nodeID string) Store {
	cfg := config.GetConfig()
	switch cfg.StorageType {
	case config.StorageTypePostgres:
		postgresConfig := PostgresConfig{
			DSN:             cfg.PostgresDSN,
			MaxIdleConns:    cfg.PostgresMaxIdleConns,
			MaxOpenConns:    cfg.PostgresMaxOpenConns,
			ConnMaxLifetime: cfg.PostgresConnMaxLifetime,
		}
		store, err := NewPostgresStore(postgresConfig)
		if err != nil {
			logger.Fatal("Failed to create postgres store", err)
		}
		logger.Info("Connected to postgres store", "node", nodeName, "node_id", nodeID)
		return store

	case config.StorageTypeBadger:
		basePath := config.DBPath()
		if basePath == "" {
			basePath = filepath.Join(".", "db")
		}
		dbPath := filepath.Join(basePath, nodeName)

		backupDir := config.BackupDir()
		if backupDir == "" {
			backupDir = filepath.Join(".", "backups")
		}

		badgerConfig := BadgerConfig{
			NodeID:              nodeName,
			EncryptionKey:       []byte(config.BadgerPassword()),
			BackupEncryptionKey: []byte(config.BadgerPassword()),
			BackupDir:           backupDir,
			DBPath:              dbPath,
		}
		badgerStore, err := NewBadgerKVStore(badgerConfig)
		if err != nil {
			logger.Fatal("Failed to create badger kv store", err)
		}
		logger.Info("Connected to badger kv store", "node", nodeName, "node_id", nodeID, "path", dbPath, "backup_dir", backupDir)
		return badgerStore

	default:
		logger.Fatal("Unsupported storage type configured", fmt.Errorf("storage type %q is not supported", config.StorageType()))
		return nil
	}
}
