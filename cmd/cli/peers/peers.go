package peers

import (
	"github.com/spf13/cobra"
)

const (
	PEERS_FILE_NAME = "peers.json"
)

// NewPeerCmd creates a new peer command group
func NewPeerCmd() *cobra.Command {
	var cmd = &cobra.Command{
		Use:   "peer",
		Short: "Peer management commands",
		Long:  "Commands for managing MPC peers and node configuration",
	}

	// Add subcommands
	cmd.AddCommand(newGeneratePeersCmd())
	cmd.AddCommand(newRegisterPeersCmd())

	return cmd
}
