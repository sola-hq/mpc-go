package types

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSigningMessage_Raw(t *testing.T) {
	msg := &SigningMessage{
		KeyType:   KeyTypeSecp256k1,
		WalletID:  "wallet-123",
		TxID:      "tx-456",
		Tx:        []byte("transaction-data"),
		Signature: []byte("signature-data"),
	}

	raw, err := msg.Raw()
	require.NoError(t, err)
	assert.NotEmpty(t, raw)

	// Verify the raw data is valid JSON and doesn't contain signature
	assert.NotContains(t, string(raw), "signature-data")
	assert.Contains(t, string(raw), "wallet-123")
	assert.Contains(t, string(raw), "secp256k1")
	assert.Contains(t, string(raw), "tx-456")
}

func TestSigningMessage_Sig(t *testing.T) {
	signature := []byte("transaction-signature")
	msg := &SigningMessage{
		KeyType:   KeyTypeEd25519,
		WalletID:  "wallet",
		TxID:      "tx",
		Tx:        []byte("tx-data"),
		Signature: signature,
	}

	assert.Equal(t, signature, msg.Sig())
}

func TestSigningMessage_InitiatorID(t *testing.T) {
	txID := "transaction-789"
	msg := &SigningMessage{
		KeyType:   KeyTypeSecp256k1,
		WalletID:  "wallet",
		TxID:      txID,
		Tx:        []byte("data"),
		Signature: []byte("sig"),
	}

	assert.Equal(t, txID, msg.InitiatorID())
}

func TestSigningMessage_RawConsistency(t *testing.T) {
	msg := &SigningMessage{
		KeyType:   KeyTypeSecp256k1,
		WalletID:  "consistent-wallet",
		TxID:      "consistent-tx",
		Tx:        []byte("consistent-data"),
		Signature: []byte("signature1"),
	}

	raw1, err1 := msg.Raw()
	require.NoError(t, err1)

	// Change signature and verify raw data remains the same
	msg.Signature = []byte("different-signature")
	raw2, err2 := msg.Raw()
	require.NoError(t, err2)

	assert.Equal(t, raw1, raw2, "Raw data should be consistent regardless of signature")
}

func TestAllMessageTypesImplementInitiatorMessage(t *testing.T) {
	var _ InitiatorMessage = &KeygenMessage{}
	var _ InitiatorMessage = &SigningMessage{}
	var _ InitiatorMessage = &ResharingMessage{}
}

func TestSigningMessage_EmptyValues(t *testing.T) {
	msg := &SigningMessage{
		KeyType:   "",
		WalletID:  "",
		TxID:      "",
		Tx:        nil,
		Signature: nil,
	}

	raw, err := msg.Raw()
	require.NoError(t, err)
	assert.NotEmpty(t, raw) // Should still produce valid JSON

	assert.Empty(t, msg.Sig())
	assert.Empty(t, msg.InitiatorID())
}

func TestGenerateKeyMessage_EmptyWallet(t *testing.T) {
	msg := &KeygenMessage{
		WalletID:  "",
		Signature: []byte("sig"),
	}

	raw, err := msg.Raw()
	require.NoError(t, err)
	assert.Equal(t, []byte(""), raw)
	assert.Equal(t, "", msg.InitiatorID())
}
