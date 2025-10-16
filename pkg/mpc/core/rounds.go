package core

import "github.com/bnb-chain/tss-lib/v2/tss"

type GetRoundFunc func(msg []byte, partyID *tss.PartyID, isBroadcast bool) (RoundInfo, error)

type RoundInfo struct {
	Index         int
	RoundMsg      string
	MsgIdentifier string
}
