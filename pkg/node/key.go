package node

type KeyInfo struct {
	ParticipantPeerIDs []string `json:"participant_peer_ids"`
	Threshold          int      `json:"threshold"`
	Version            int      `json:"version"`
}

type KeyStore interface {
	Get(walletID string) (*KeyInfo, error)
	Save(walletID string, info *KeyInfo) error
}
