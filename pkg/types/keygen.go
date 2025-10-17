package types

type KeygenMessage struct {
	WalletID  string `json:"wallet_id"`
	Signature []byte `json:"signature"`
}

type KeygenResponse struct {
	WalletID    string `json:"wallet_id"`
	ECDSAPubKey []byte `json:"ecdsa_pub_key"`
	EDDSAPubKey []byte `json:"eddsa_pub_key"`

	ErrorCode   ErrorCode `json:"error_code"`
	ErrorReason string    `json:"error_reason"`
}
