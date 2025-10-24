package storage

import (
	"errors"

	"github.com/dgraph-io/badger/v4"
	"github.com/dgraph-io/badger/v4/options"
	"github.com/fystack/mpcium/pkg/logger"
)

var (
	ErrEncryptionKeyNotProvided       = errors.New("encryption key not provided")
	ErrBackupEncryptionKeyNotProvided = errors.New("backup encryption key not provided")
)

// BadgerStore is a Storage implementation backed by BadgerDB.
type BadgerStore struct {
	DB             *badger.DB
	BackupExecutor *badgerBackupExecutor
}

type BadgerConfig struct {
	NodeID              string
	EncryptionKey       []byte
	BackupEncryptionKey []byte
	BackupDir           string
	DBPath              string
}

// NewBadgerStore creates a new BadgerStore instance.
func NewBadgerStore(config BadgerConfig) (*BadgerStore, error) {
	// must ensure encryption key is provided
	if len(config.EncryptionKey) == 0 {
		return nil, ErrEncryptionKeyNotProvided
	}
	if len(config.BackupEncryptionKey) == 0 {
		return nil, ErrBackupEncryptionKeyNotProvided
	}

	opts := badger.DefaultOptions(config.DBPath).
		WithCompression(options.ZSTD).
		WithEncryptionKey(config.EncryptionKey).
		WithIndexCacheSize(16 << 20).
		WithBlockCacheSize(32 << 20).
		WithSyncWrites(true).
		WithVerifyValueChecksum(true). // validate every value-log entry's checksum on read, surfacing corruption instead of masking it
		WithCompactL0OnClose(true).    // compacts level-0 SSTables on shutdown, reducing startup work and avoiding stalls on open
		WithLogger(newQuietBadgerLogger())

	db, err := badger.Open(opts)
	if err != nil {
		return nil, err
	}

	logger.Info("Connected to BadgerDB successfully!", "path", config.DBPath)

	backupExecutor := NewBadgerBackupExecutor(
		config.NodeID,
		db,
		config.BackupEncryptionKey,
		config.BackupDir,
	)

	return &BadgerStore{DB: db, BackupExecutor: backupExecutor}, nil
}

// Put stores a key-value pair in the BadgerDB.
func (b *BadgerStore) Put(key string, value []byte) error {
	return b.DB.Update(func(txn *badger.Txn) error {
		return txn.Set([]byte(key), value)
	})
}

// Get retrieves the value associated with a key from BadgerDB.
func (b *BadgerStore) Get(key string) ([]byte, error) {
	var result []byte
	err := b.DB.View(func(txn *badger.Txn) error {
		item, err := txn.Get([]byte(key))
		if err == nil {
			return item.Value(func(val []byte) error {
				result = append([]byte{}, val...)
				return nil
			})
		}
		return err
	})

	return result, err
}

func (b *BadgerStore) Keys() ([]string, error) {
	var keys []string
	err := b.DB.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.PrefetchValues = false
		it := txn.NewIterator(opts)
		defer it.Close()

		for it.Rewind(); it.Valid(); it.Next() {
			item := it.Item()
			keys = append(keys, string(item.Key()))
		}
		return nil
	})

	return keys, err
}

// Delete removes a key-value pair from BadgerDB.
func (b *BadgerStore) Delete(key string) error {
	return b.DB.Update(func(txn *badger.Txn) error {
		return txn.Delete([]byte(key))
	})
}

func (b *BadgerStore) Backup() error {
	if b.BackupExecutor == nil {
		return errors.New("backup executor is not initialized")
	}
	return b.BackupExecutor.Execute()
}

// Close closes the BadgerDB.
func (b *BadgerStore) Close() error {
	return b.DB.Close()
}
