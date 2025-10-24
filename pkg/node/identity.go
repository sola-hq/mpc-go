package node

import "github.com/fystack/mpcium/pkg/types"

// NodeIdentity represents a node's identity metadata.
type NodeIdentity struct {
	NodeName  string `json:"node_name"`
	NodeID    string `json:"node_id"`
	PublicKey string `json:"public_key"`
	CreatedAt string `json:"created_at"`
}

// Store defines the behavior required by MPC components to manage node
// identities and message authentication.
type IdentityStore interface {
	GetPublicKey(nodeID string) ([]byte, error)
	VerifyInitiatorMessage(msg types.InitiatorMessage) error
	SignMessage(msg *types.TssMessage) ([]byte, error)
	VerifyMessage(msg *types.TssMessage) error

	SignEcdhMessage(msg *types.ECDHMessage) ([]byte, error)
	VerifySignature(msg *types.ECDHMessage) error

	SetSymmetricKey(peerID string, key []byte)
	GetSymmetricKey(peerID string) ([]byte, error)
	RemoveSymmetricKey(peerID string)
	GetSymmetricKeyCount() int
	CheckSymmetricKeyComplete(desired int) bool

	EncryptMessage(plaintext []byte, peerID string) ([]byte, error)
	DecryptMessage(cipher []byte, peerID string) ([]byte, error)
}
