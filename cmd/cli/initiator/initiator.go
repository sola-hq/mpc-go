package initiator

import (
	"github.com/spf13/cobra"
)

// NewInitiatorCmd creates a new initiator command group
func NewInitiatorCmd() *cobra.Command {
	var cmd = &cobra.Command{
		Use:   "initiator",
		Short: "Initiator management commands",
		Long:  "Commands for generating and managing event initiator identities",
	}

	// Add subcommands
	cmd.AddCommand(NewGenerateInitiatorCmd())

	return cmd
}
