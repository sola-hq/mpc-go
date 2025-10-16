package core

import (
	"bytes"
	"fmt"
	"math/big"
	"strings"

	"github.com/bnb-chain/tss-lib/v2/tss"
	"github.com/google/uuid"
)

func PartyIDToNodeID(partyID *tss.PartyID) string {
	if partyID == nil {
		return ""
	}
	nodeID, _, _ := strings.Cut(string(partyID.KeyInt().Bytes()), ":")
	return strings.TrimSpace(nodeID)
}

func PartyIDsToNodeIDs(pids []*tss.PartyID) []string {
	out := make([]string, 0, len(pids))
	for _, p := range pids {
		out = append(out, PartyIDToNodeID(p))
	}
	return out
}

func ComparePartyIDs(x, y *tss.PartyID) bool {
	return bytes.Equal(x.KeyInt().Bytes(), y.KeyInt().Bytes())
}

// CreatePartyID creates a new party ID for the given node ID, label and version
// It returns the party ID: random string
// Moniker: for routing messages
// Key: for mpc internal use (need persistent storage)
func CreatePartyID(nodeID string, label string, version int) *tss.PartyID {
	partyID := uuid.NewString()
	var key *big.Int
	if version == BackwardCompatibleVersion {
		key = new(big.Int).SetBytes([]byte(nodeID))
	} else {
		key = new(big.Int).SetBytes([]byte(fmt.Sprintf("%s:%d", nodeID, version)))
	}
	return tss.NewPartyID(partyID, label, key)
}
