package main

import (
	"path/filepath"

	"github.com/fystack/mpcium/pkg/config"
	"github.com/fystack/mpcium/pkg/kvstore"
	"github.com/fystack/mpcium/pkg/logger"
	"github.com/spf13/viper"
)

func NewBadgerKV(nodeName, nodeID string, appConfig *config.AppConfig) *kvstore.BadgerKVStore {
	// Badger KV DB
	// Use configured db_path or default to current directory + "db"
	basePath := viper.GetString("db_path")
	if basePath == "" {
		basePath = filepath.Join(".", "db")
	}
	dbPath := filepath.Join(basePath, nodeName)

	// Use configured backup_dir or default to current directory + "backups"
	backupDir := viper.GetString("backup_dir")
	if backupDir == "" {
		backupDir = filepath.Join(".", "backups")
	}

	// Create BadgerConfig struct
	config := kvstore.BadgerConfig{
		NodeID:              nodeName,
		EncryptionKey:       []byte(appConfig.BadgerPassword),
		BackupEncryptionKey: []byte(appConfig.BadgerPassword), // Using same key for backup encryption
		BackupDir:           backupDir,
		DBPath:              dbPath,
	}

	badgerKv, err := kvstore.NewBadgerKVStore(config)
	if err != nil {
		logger.Fatal("Failed to create badger kv store", err)
	}
	logger.Info("Connected to badger kv store", "path", dbPath, "backup_dir", backupDir)
	return badgerKv
}
