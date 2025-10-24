package main

import (
	"path/filepath"

	"github.com/fystack/mpcium/pkg/config"
	"github.com/fystack/mpcium/pkg/kvstore"
	"github.com/fystack/mpcium/pkg/logger"
)

func NewBadgerKV(nodeName, nodeID string) *kvstore.BadgerKVStore {
	// Badger KV DB
	// Use configured db_path or default to current directory + "db"
	basePath := config.DBPath()
	if basePath == "" {
		basePath = filepath.Join(".", "db")
	}
	dbPath := filepath.Join(basePath, nodeName)

	// Use configured backup_dir or default to current directory + "backups"
	backupDir := config.BackupDir()
	if backupDir == "" {
		backupDir = filepath.Join(".", "backups")
	}

	// Create BadgerConfig struct
	badgerConfig := kvstore.BadgerConfig{
		NodeID:              nodeName,
		EncryptionKey:       []byte(config.BadgerPassword()),
		BackupEncryptionKey: []byte(config.BadgerPassword()), // Using same key for backup encryption
		BackupDir:           backupDir,
		DBPath:              dbPath,
	}

	badgerKv, err := kvstore.NewBadgerKVStore(badgerConfig)
	if err != nil {
		logger.Fatal("Failed to create badger kv store", err)
	}
	logger.Info("Connected to badger kv store", "path", dbPath, "backup_dir", backupDir)
	return badgerKv
}
