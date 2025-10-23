package types

import (
	"context"
	"encoding/json"
)

type KeyType string

const (
	KeyTypeSecp256k1 KeyType = "secp256k1"
	KeyTypeEd25519   KeyType = "ed25519"
)

type EventInitiatorKeyType string

const (
	EventInitiatorKeyTypeEd25519 EventInitiatorKeyType = "ed25519"
	EventInitiatorKeyTypeP256    EventInitiatorKeyType = "p256"
)

type Initiator interface {
	CreateWallet(walletID string) error
	OnWalletCreationResult(callback func(event KeygenResponse)) error

	SignTransaction(msg *SigningMessage) error
	OnSignResult(callback func(event SigningResponse)) error
	SignTransactionSync(ctx context.Context, msg *SigningMessage) (*SigningResponse, error)

	Resharing(msg *ResharingMessage) error
	OnResharingResult(callback func(result ResharingResponse)) error
}

// InitiatorMessage is anything that carries a payload to verify and its signature.
type InitiatorMessage interface {
	// Raw returns the canonical byte‐slice that was signed.
	Raw() ([]byte, error)
	// Sig returns the signature over Raw().
	Sig() []byte
	// InitiatorID returns the ID whose public key we have to look up.
	InitiatorID() string
}

func (m *SigningMessage) Raw() ([]byte, error) {
	// omit the Signature field itself when computing the signed‐over data
	payload := struct {
		KeyType  KeyType `json:"key_type"`
		WalletID string  `json:"wallet_id"`
		TxID     string  `json:"tx_id"`
		Tx       []byte  `json:"tx"`
	}{
		KeyType:  m.KeyType,
		WalletID: m.WalletID,
		TxID:     m.TxID,
		Tx:       m.Tx,
	}
	return json.Marshal(payload)
}

func (m *SigningMessage) Sig() []byte {
	return m.Signature
}

func (m *SigningMessage) InitiatorID() string {
	return m.TxID
}

func (m *KeygenMessage) Raw() ([]byte, error) {
	return []byte(m.WalletID), nil
}

func (m *KeygenMessage) Sig() []byte {
	return m.Signature
}

func (m *KeygenMessage) InitiatorID() string {
	return m.WalletID
}

func (m *ResharingMessage) Raw() ([]byte, error) {
	copy := *m           // create a shallow copy
	copy.Signature = nil // modify only the copy
	return json.Marshal(&copy)
}

func (m *ResharingMessage) Sig() []byte {
	return m.Signature
}

func (m *ResharingMessage) InitiatorID() string {
	return m.WalletID
}
