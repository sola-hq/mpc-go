package main

import (
	"context"
	"fmt"
	"os"

	"github.com/fystack/mpcium/cmd/cli/benchmark"
	"github.com/fystack/mpcium/cmd/cli/identity"
	"github.com/fystack/mpcium/cmd/cli/initiator"
	"github.com/fystack/mpcium/cmd/cli/peers"
	"github.com/fystack/mpcium/cmd/cli/recovery"
	"github.com/spf13/cobra"
)

const (
	// Version information
	VERSION = "0.2.1"
)

var (
	configFile string
)

func main() {
	ctx := context.Background()

	// Initialize commands with context
	rootCmd.AddCommand(peers.NewPeerCmd())
	rootCmd.AddCommand(identity.NewIdentityCmd(ctx))
	rootCmd.AddCommand(initiator.NewInitiatorCmd())
	rootCmd.AddCommand(benchmark.NewBenchmarkCmd())
	rootCmd.AddCommand(recovery.NewRecoveryCmd())
	rootCmd.AddCommand(versionCmd)

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

var rootCmd = &cobra.Command{
	Use:   "mpc-cli",
	Short: "MPC CLI",
	Long:  "MPC CLI for peer, identity, and initiator configuration",
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		// Global configuration can be loaded here if needed
	},
}

func init() {
	rootCmd.PersistentFlags().StringVarP(&configFile, "config", "c", "", "Path to configuration file")
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Display detailed version information",
	Long:  "Display detailed version information",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("mpc-cli version %s\n", VERSION)
	},
}
