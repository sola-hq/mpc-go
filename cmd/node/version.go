package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

const (
	Version = "0.3.2"
)

func NewVersionCmd() *cobra.Command {
	var cmd = &cobra.Command{
		Use:   "version",
		Short: "Display version",
		Long:  "Display version",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("mpc-node version %s\n", Version)
		},
	}
	return cmd
}
