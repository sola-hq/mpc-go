package messaging

const (
	StreamName = "mpc"

	KeygenRequestTopic    = "mpc.keygen_request.*"
	SigningRequestTopic   = "mpc.signing_request.*"
	ResharingRequestTopic = "mpc.resharing_request.*"

	KeygenResultTopic    = "mpc.mpc_keygen_result.*"
	SigningResultTopic   = "mpc.mpc_signing_result.*"
	ResharingResultTopic = "mpc.mpc_resharing_result.*"

	KeygenResultCompleteTopic    = "mpc.mpc_keygen_result.complete"
	SigningResultCompleteTopic   = "mpc.mpc_signing_result.complete"
	ResharingResultCompleteTopic = "mpc.mpc_resharing_result.complete"

	KeygenBrokerStream      = "mpc-keygen"
	KeygenConsumerStream    = "mpc-keygen-consumer"
	SigningBrokerStream     = "mpc-signing"
	SigningConsumerStream   = "mpc-signing-consumer"
	ResharingBrokerStream   = "mpc-resharing"
	ResharingConsumerStream = "mpc-resharing-consumer"

	KeygenResultQueueName    = "mpc_keygen_result"
	SigningResultQueueName   = "mpc_signing_result"
	ResharingResultQueueName = "mpc_resharing_result"
)

func FormatKeygenRequestTopic(walletID string) string {
	return "mpc.keygen_request." + walletID
}

func FormatSigningRequestTopic(txID string) string {
	return "mpc.signing_request." + txID
}

func FormatResharingRequestTopic(walletID string) string {
	return "mpc.resharing_request." + walletID
}

func FormatKeygenResultTopic(walletID string) string {
	return "mpc.mpc_keygen_result." + walletID
}

func FormatSigningResultTopic(txID string) string {
	return "mpc.mpc_signing_result." + txID
}

func FormatResharingResultTopic(walletID string) string {
	return "mpc.mpc_resharing_result." + walletID
}
