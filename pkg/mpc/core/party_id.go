package core

import (
	"bytes"
	"strings"

	"github.com/bnb-chain/tss-lib/v2/tss"
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
