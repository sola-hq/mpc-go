package constant

// MPC Topic Constants

const (
	// MPC stream name
	StreamName = "mpc"

	// Request topics (with wildcards)
	KeygenRequestTopic    = "mpc.keygen_request.*"
	SigningRequestTopic   = "mpc.signing_request.*"
	ResharingRequestTopic = "mpc.resharing_request.*"

	// Result topics (with wildcards)
	KeygenResultTopic    = "mpc.mpc_keygen_result.*"
	SigningResultTopic   = "mpc.mpc_signing_result.*"
	ResharingResultTopic = "mpc.mpc_resharing_result.*"

	// Specific result topics (without wildcards)
	KeygenResultCompleteTopic    = "mpc.mpc_keygen_result.complete"
	SigningResultCompleteTopic   = "mpc.mpc_signing_result.complete"
	ResharingResultCompleteTopic = "mpc.mpc_resharing_result.complete"

	// Broker stream names
	KeygenBrokerStream      = "mpc-keygen"
	KeygenConsumerStream    = "mpc-keygen-consumer"
	SigningBrokerStream     = "mpc-signing"
	SigningConsumerStream   = "mpc-signing-consumer"
	ResharingBrokerStream   = "mpc-resharing"
	ResharingConsumerStream = "mpc-resharing-consumer"

	// Message queue names
	KeygenResultQueueName    = "mpc_keygen_result"
	SigningResultQueueName   = "mpc_signing_result"
	ResharingResultQueueName = "mpc_resharing_result"
)

// Topic Format Functions
// FormatKeygenRequestTopic creates a specific keygen request topic for a wallet ID
func FormatKeygenRequestTopic(walletID string) string {
	return "mpc.keygen_request." + walletID
}

// FormatSigningRequestTopic creates a specific signing request topic for a transaction ID
func FormatSigningRequestTopic(txID string) string {
	return "mpc.signing_request." + txID
}

// FormatResharingRequestTopic creates a specific reshare request topic for a wallet ID
func FormatResharingRequestTopic(walletID string) string {
	return "mpc.resharing_request." + walletID
}

// FormatKeygenResultTopic creates a specific keygen result topic for a wallet ID
func FormatKeygenResultTopic(walletID string) string {
	return "mpc.mpc_keygen_result." + walletID
}

// FormatSigningResultTopic creates a specific signing result topic for a transaction ID
func FormatSigningResultTopic(txID string) string {
	return "mpc.mpc_signing_result." + txID
}

// FormatResharingResultTopic creates a specific resharing result topic for a wallet ID
func FormatResharingResultTopic(walletID string) string {
	return "mpc.mpc_resharing_result." + walletID
}
