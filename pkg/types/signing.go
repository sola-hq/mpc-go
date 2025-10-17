package types

type SigningMessage struct {
	KeyType   KeyType `json:"key_type"`
	WalletID  string  `json:"wallet_id"`
	TxID      string  `json:"tx_id"`
	Tx        []byte  `json:"tx"`
	Signature []byte  `json:"signature"`
}

type SigningResponse struct {
	ErrorCode         ErrorCode `json:"error_code"`
	ErrorReason       string    `json:"error_reason"`
	IsTimeout         bool      `json:"is_timeout"`
	WalletID          string    `json:"wallet_id"`
	TxID              string    `json:"tx_id"`
	R                 []byte    `json:"r"`
	S                 []byte    `json:"s"`
	SignatureRecovery []byte    `json:"signature_recovery"`

	// TODO: define two separate events for eddsa and ecdsa
	Signature []byte `json:"signature"`
}

type SigningResultSuccessEvent struct {
	WalletID          string `json:"wallet_id"`
	TxID              string `json:"tx_id"`
	R                 []byte `json:"r"`
	S                 []byte `json:"s"`
	SignatureRecovery []byte `json:"signature_recovery"`

	// TODO: define two separate events for eddsa and ecdsa
	Signature []byte `json:"signature"`
}

type SigningResultErrorEvent struct {
	WalletID    string    `json:"wallet_id"`
	TxID        string    `json:"tx_id"`
	ErrorCode   ErrorCode `json:"error_code"`
	ErrorReason string    `json:"error_reason"`
	IsTimeout   bool      `json:"is_timeout"`
}
