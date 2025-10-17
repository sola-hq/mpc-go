package eddsa

import (
	"github.com/bnb-chain/tss-lib/v2/eddsa/keygen"
	"github.com/bnb-chain/tss-lib/v2/eddsa/resharing"
	"github.com/bnb-chain/tss-lib/v2/eddsa/signing"
	"github.com/bnb-chain/tss-lib/v2/tss"
	"github.com/fystack/mpcium/pkg/common/errors"
	"github.com/fystack/mpcium/pkg/mpc/core"
)

const (
	KEYGEN1            = "KGRound1Message"
	KEYGEN2aUnicast    = "KGRound2Message1"
	KEYGEN2b           = "KGRound2Message2"
	KEYSIGN1           = "SignRound1Message"
	KEYSIGN2           = "SignRound2Message"
	KEYSIGN3           = "SignRound3Message"
	RESHARING1         = "DGRound1Message"
	RESHARING2         = "DGRound2Message"
	RESHARING3aUnicast = "DGRound3Message1"
	RESHARING3bUnicast = "DGRound3Message2"
	RESHARING4         = "DGRound4Message"

	TSSKEYGENROUNDS    = 3
	TSSKEYSIGNROUNDS   = 3
	TSSRESHARINGROUNDS = 4
)

func GetMsgRound(msg []byte, partyID *tss.PartyID, isBroadcast bool) (core.RoundInfo, error) {
	parsedMsg, err := tss.ParseWireMessage(msg, partyID, isBroadcast)
	if err != nil {
		return core.RoundInfo{}, err
	}
	switch parsedMsg.Content().(type) {
	case *keygen.KGRound1Message:
		return core.RoundInfo{
			Index:    0,
			RoundMsg: KEYGEN1,
		}, nil

	case *keygen.KGRound2Message1:
		return core.RoundInfo{
			Index:    1,
			RoundMsg: KEYGEN2aUnicast,
		}, nil

	case *keygen.KGRound2Message2:
		return core.RoundInfo{
			Index:    2,
			RoundMsg: KEYGEN2b,
		}, nil

	case *signing.SignRound1Message:
		return core.RoundInfo{
			Index:    0,
			RoundMsg: KEYSIGN1,
		}, nil

	case *signing.SignRound2Message:
		return core.RoundInfo{
			Index:    0,
			RoundMsg: KEYSIGN2,
		}, nil

	case *signing.SignRound3Message:
		return core.RoundInfo{
			Index:    0,
			RoundMsg: KEYSIGN3,
		}, nil

	case *resharing.DGRound1Message:
		return core.RoundInfo{
			Index:    0,
			RoundMsg: RESHARING1,
		}, nil

	case *resharing.DGRound2Message:
		return core.RoundInfo{
			Index:    1,
			RoundMsg: RESHARING2,
		}, nil

	case *resharing.DGRound3Message1:
		return core.RoundInfo{
			Index:    2,
			RoundMsg: RESHARING3aUnicast,
		}, nil

	case *resharing.DGRound3Message2:
		return core.RoundInfo{
			Index:    3,
			RoundMsg: RESHARING3bUnicast,
		}, nil

	case *resharing.DGRound4Message:
		return core.RoundInfo{
			Index:    4,
			RoundMsg: RESHARING4,
		}, nil

	default:
		return core.RoundInfo{}, errors.New("unknown round")
	}
}
