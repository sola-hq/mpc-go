package main

import (
	"context"
	"time"

	"github.com/fystack/mpcium/pkg/logger"
	"github.com/fystack/mpcium/pkg/storage"
)

const (
	DefaultBackupPeriodSeconds = 300 // (5 minutes)
)

func StartPeriodicBackup(ctx context.Context, badgerKV *storage.BadgerKVStore, periodSeconds int) func() {
	if periodSeconds <= 0 {
		periodSeconds = DefaultBackupPeriodSeconds
	}
	backupTicker := time.NewTicker(time.Duration(periodSeconds) * time.Second)
	backupCtx, backupCancel := context.WithCancel(ctx)
	go func() {
		for {
			select {
			case <-backupCtx.Done():
				logger.Info("Backup background job stopped")
				return
			case <-backupTicker.C:
				logger.Info("Running periodic BadgerDB backup...")
				err := badgerKV.Backup()
				if err != nil {
					logger.Error("Periodic BadgerDB backup failed", err)
				} else {
					logger.Info("Periodic BadgerDB backup completed successfully")
				}
			}
		}
	}()
	return backupCancel
}
