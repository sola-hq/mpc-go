package identity

import (
	"context"

	"github.com/spf13/cobra"
)

// NewIdentityCmd creates a new identity command group
func NewIdentityCmd(ctx context.Context) *cobra.Command {
	var cmd = &cobra.Command{
		Use:   "identity",
		Short: "Identity management commands",
		Long:  "Commands for generating and managing node identities",
	}

	// Add subcommands
	cmd.AddCommand(NewGenerateIdentityCmd(ctx))

	return cmd
}
