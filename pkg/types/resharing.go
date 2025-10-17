package types

type ResharingMessage struct {
	SessionID    string   `json:"session_id"`
	NodeIDs      []string `json:"node_ids"` // new peer IDs
	NewThreshold int      `json:"new_threshold"`
	KeyType      KeyType  `json:"key_type"`
	WalletID     string   `json:"wallet_id"`
	Signature    []byte   `json:"signature,omitempty"`
}

type ResharingResponse struct {
	WalletID     string  `json:"wallet_id"`
	NewThreshold int     `json:"new_threshold"`
	KeyType      KeyType `json:"key_type"`
	PubKey       []byte  `json:"pub_key"`

	ErrorCode   ErrorCode `json:"error_code"`
	ErrorReason string    `json:"error_reason"`
}
