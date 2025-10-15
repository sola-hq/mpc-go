package event

// MPC Topic Constants
const (
	// Request Topics
	KeygenRequestTopic  = "mpc.keygen_request.*"
	SigningRequestTopic = "mpc.signing_request.*"
	ReshareRequestTopic = "mpc.reshare_request.*"

	// Result Topics (with wildcards)
	KeygenResultTopic  = "mpc.mpc_keygen_result.*"
	SigningResultTopic = "mpc.mpc_signing_result.*"
	ReshareResultTopic = "mpc.mpc_reshare_result.*"

	// Specific Result Topics (without wildcards)
	KeygenResultCompleteTopic  = "mpc.mpc_keygen_result.complete"
	SigningResultCompleteTopic = "mpc.mpc_signing_result.complete"
	ReshareResultCompleteTopic = "mpc.mpc_reshare_result.complete"

	// Stream Names
	KeygenBrokerStream    = "mpc-keygen"
	KeygenConsumerStream  = "mpc-keygen-consumer"
	SigningBrokerStream   = "mpc-signing"
	SigningConsumerStream = "mpc-signing-consumer"
	ReshareBrokerStream   = "mpc-reshare"
	ReshareConsumerStream = "mpc-reshare-consumer"

	// Message Queue Names
	KeygenResultQueueName  = "mpc_keygen_result"
	SigningResultQueueName = "mpc_signing_result"
	ReshareResultQueueName = "mpc_reshare_result"
)

// Topic Format Functions

// FormatKeygenResultTopic creates a specific keygen result topic for a wallet ID
func FormatKeygenResultTopic(walletID string) string {
	return "mpc.mpc_keygen_result." + walletID
}

// FormatSigningResultTopic creates a specific signing result topic for a transaction ID
func FormatSigningResultTopic(txID string) string {
	return "mpc.mpc_signing_result." + txID
}

// FormatReshareResultTopic creates a specific reshare result topic for a wallet ID
func FormatReshareResultTopic(walletID string) string {
	return "mpc.mpc_reshare_result." + walletID
}

// FormatKeygenRequestTopic creates a specific keygen request topic for a wallet ID
func FormatKeygenRequestTopic(walletID string) string {
	return "mpc.keygen_request." + walletID
}

// FormatSigningRequestTopic creates a specific signing request topic for a transaction ID
func FormatSigningRequestTopic(txID string) string {
	return "mpc.signing_request." + txID
}

// FormatReshareRequestTopic creates a specific reshare request topic for a wallet ID
func FormatReshareRequestTopic(walletID string) string {
	return "mpc.reshare_request." + walletID
}
