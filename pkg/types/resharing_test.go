package types

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResharingMessage_Raw(t *testing.T) {
	msg := &ResharingMessage{
		NodeIDs:      []string{"node1", "node2", "node3"},
		NewThreshold: 2,
		KeyType:      KeyTypeEd25519,
		WalletID:     "resharing-wallet",
		Signature:    []byte("resharing-signature"),
	}

	raw, err := msg.Raw()
	require.NoError(t, err)

	type data struct {
		SessionID    string   `json:"session_id"`
		NodeIDs      []string `json:"node_ids"` // new peer IDs
		NewThreshold int      `json:"new_threshold"`
		KeyType      KeyType  `json:"key_type"`
		WalletID     string   `json:"wallet_id"`
	}

	d := data{
		SessionID:    msg.SessionID,
		NodeIDs:      msg.NodeIDs,
		NewThreshold: msg.NewThreshold,
		KeyType:      msg.KeyType,
		WalletID:     msg.WalletID,
	}

	expectedBytes, err := json.Marshal(d)
	require.NoError(t, err)
	assert.Equal(t, expectedBytes, raw)
}

func TestResharingMessage_Sig(t *testing.T) {
	signature := []byte("resharing-signature")
	msg := &ResharingMessage{
		NodeIDs:      []string{"node1", "node2"},
		NewThreshold: 1,
		KeyType:      KeyTypeSecp256k1,
		WalletID:     "wallet",
		Signature:    signature,
	}

	assert.Equal(t, signature, msg.Sig())
}

func TestResharingMessage_InitiatorID(t *testing.T) {
	walletID := "resharing-wallet-123"
	msg := &ResharingMessage{
		NodeIDs:      []string{"node1"},
		NewThreshold: 0,
		KeyType:      KeyTypeEd25519,
		WalletID:     walletID,
		Signature:    []byte("sig"),
	}

	assert.Equal(t, walletID, msg.InitiatorID())
}
