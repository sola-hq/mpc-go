package main

import (
	"fmt"
	"os"
	"syscall"

	"github.com/dgraph-io/badger/v4"
	"github.com/dgraph-io/badger/v4/options"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

var (
	dbPath string
)

var rootCmd = &cobra.Command{
	Use:   "print-keys",
	Short: "Print all keys from a BadgerDB database",
	Long:  "Print all keys from a BadgerDB database",
	RunE:  printKeys,
}

func init() {
	rootCmd.Flags().StringVarP(&dbPath, "db-path", "p", "", "Path to the BadgerDB database directory (required)")
	_ = rootCmd.MarkFlagRequired("db-path")
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func printKeys(cmd *cobra.Command, args []string) error {

	// Prompt for password
	fmt.Print("Enter database password: ")
	passwordBytes, err := term.ReadPassword(int(syscall.Stdin))
	if err != nil {
		return fmt.Errorf("failed to read password: %v", err)
	}
	fmt.Println() // Print newline after password input
	password := string(passwordBytes)

	// Configure BadgerDB options
	opts := badger.DefaultOptions(dbPath).
		WithCompression(options.ZSTD).
		WithEncryptionKey([]byte(password)).
		WithIndexCacheSize(16 << 20).
		WithBlockCacheSize(32 << 20).
		WithReadOnly(true) // Open in read-only mode for safety

	// Open the database
	db, err := badger.Open(opts)
	if err != nil {
		return fmt.Errorf("failed to open BadgerDB: %v", err)
	}
	defer db.Close()

	fmt.Printf("Opening database at: %s\n", dbPath)
	fmt.Println("=== All Keys in Database ===")

	// Iterate through all keys
	err = db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.PrefetchValues = false // We only need keys, not values
		it := txn.NewIterator(opts)
		defer it.Close()

		count := 0
		for it.Rewind(); it.Valid(); it.Next() {
			item := it.Item()
			key := string(item.Key())
			count++
			fmt.Printf("%d. %s\n", count, key)
		}

		if count == 0 {
			fmt.Println("No keys found in the database.")
		} else {
			fmt.Printf("\nTotal keys: %d\n", count)
		}

		return nil
	})

	if err != nil {
		return fmt.Errorf("failed to iterate over database: %v", err)
	}

	return nil
}
