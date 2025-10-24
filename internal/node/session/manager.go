package session

import (
	"fmt"
	"maps"
	"sync"
	"time"

	"github.com/fystack/mpcium/pkg/logger"
)

func GetSessionID(walletID, txID string) string {
	return fmt.Sprintf("%s-%s", walletID, txID)
}

// Manager is the session manager
type Manager struct {
	sessions        map[string]time.Time // Maps "walletID-txID" to creation time
	mu              sync.RWMutex
	cleanupInterval time.Duration // How often to run cleanup
	sessionTimeout  time.Duration // How long before a session is considered stale
	stopChan        chan struct{} // Signal to stop session cleanup routine
}

// NewManager creates a new session manager
func NewManager(cleanupInterval, sessionTimeout time.Duration) *Manager {
	return &Manager{
		sessions:        make(map[string]time.Time),
		cleanupInterval: cleanupInterval,
		sessionTimeout:  sessionTimeout,
		stopChan:        make(chan struct{}),
	}
}

// AddSession adds a session
func (m *Manager) AddSession(walletID, txID string) {
	sessionID := GetSessionID(walletID, txID)
	m.mu.Lock()
	defer m.mu.Unlock()

	m.sessions[sessionID] = time.Now()
	logger.Debug("Session added", "sessionID", sessionID)
}

// HasSession checks if a session already exists
func (m *Manager) HasSession(walletID, txID string) bool {
	sessionID := GetSessionID(walletID, txID)

	m.mu.RLock()
	_, exists := m.sessions[sessionID]
	m.mu.RUnlock()

	if exists {
		logger.Info("Session already exists", "walletID", walletID, "txID", txID)
		return true
	}

	return false
}

// StartCleanup starts the session cleanup routine
func (m *Manager) StartCleanup() {
	ticker := time.NewTicker(m.cleanupInterval)
	defer ticker.Stop()

	logger.Info("Session cleanup routine started",
		"cleanupInterval", m.cleanupInterval,
		"sessionTimeout", m.sessionTimeout)

	for {
		select {
		case <-ticker.C:
			m.cleanupStaleSessions()
		case <-m.stopChan:
			logger.Info("Session cleanup routine stopped")
			return
		}
	}
}

// Stop stops the session cleanup routine
func (m *Manager) Stop() {
	close(m.stopChan)
}

// cleanupStaleSessions cleans up stale sessions
func (m *Manager) cleanupStaleSessions() {
	now := time.Now()
	m.mu.Lock()
	defer m.mu.Unlock()

	cleanedCount := 0
	for sessionID, creationTime := range m.sessions {
		if now.Sub(creationTime) > m.sessionTimeout {
			delete(m.sessions, sessionID)
			cleanedCount++
		}
	}

	if cleanedCount > 0 {
		logger.Info("Cleaned up stale sessions",
			"cleanedCount", cleanedCount,
			"remainingCount", len(m.sessions))
	}
}

// GetActiveSessionCount gets the number of active sessions
func (m *Manager) GetActiveSessionCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.sessions)
}

// GetActiveSessions gets all active sessions (for debugging)
func (m *Manager) GetActiveSessions() map[string]time.Time {
	m.mu.RLock()
	defer m.mu.RUnlock()
	// return a copy to avoid external modification
	sessions := make(map[string]time.Time)
	maps.Copy(sessions, m.sessions)
	return sessions
}
