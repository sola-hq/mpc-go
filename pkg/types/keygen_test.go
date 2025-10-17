package types

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestKeygenMessage_Raw(t *testing.T) {
	msg := &KeygenMessage{
		WalletID:  "test-wallet-123",
		Signature: []byte("test-signature"),
	}

	raw, err := msg.Raw()
	require.NoError(t, err)
	assert.Equal(t, []byte("test-wallet-123"), raw)
}

func TestKeygenMessage_Sig(t *testing.T) {
	signature := []byte("test-signature-bytes")
	msg := &KeygenMessage{
		WalletID:  "test-wallet",
		Signature: signature,
	}

	assert.Equal(t, signature, msg.Sig())
}

func TestKeygenMessage_InitiatorID(t *testing.T) {
	walletID := "test-wallet-456"
	msg := &KeygenMessage{
		WalletID:  walletID,
		Signature: []byte("signature"),
	}

	assert.Equal(t, walletID, msg.InitiatorID())
}
