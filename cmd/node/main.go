package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

func main() {
	var rootCmd = &cobra.Command{
		Use:   "mpc-node",
		Short: "MPC Node",
		Long:  "Multi-Party Computation node for threshold signatures",
	}

	rootCmd.AddCommand(NewStartCmd())
	rootCmd.AddCommand(NewVersionCmd())

	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
