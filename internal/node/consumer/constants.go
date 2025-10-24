package consumer

import (
	"fmt"
	"time"
)

// Event topics
const (
	MPCGenerateEvent  = "mpc:generate"
	MPCSignEvent      = "mpc:signing"
	MPCResharingEvent = "mpc:resharing"
)

// Timing constants
const (
	ReadinessCheckInterval = 2 * time.Second
)

// BuildIdempotentKey creates an idempotent key for different MPC operation types
func BuildIdempotentKey(baseID string, sessionID string, formatTemplate string) string {
	var uniqueKey string
	if sessionID != "" {
		uniqueKey = fmt.Sprintf("%s:%s", baseID, sessionID)
	} else {
		uniqueKey = baseID
	}
	return fmt.Sprintf(formatTemplate, uniqueKey)
}
