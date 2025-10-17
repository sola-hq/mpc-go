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
	activeSessions  map[string]time.Time // Maps "walletID-txID" to creation time
	sessionsLock    sync.RWMutex
	cleanupInterval time.Duration // How often to run cleanup
	sessionTimeout  time.Duration // How long before a session is considered stale
	stopChan        chan struct{} // Signal to stop session cleanup routine
}

// NewManager creates a new session manager
func NewManager(cleanupInterval, sessionTimeout time.Duration) *Manager {
	return &Manager{
		activeSessions:  make(map[string]time.Time),
		cleanupInterval: cleanupInterval,
		sessionTimeout:  sessionTimeout,
		stopChan:        make(chan struct{}),
	}
}

// AddSession adds a session
func (m *Manager) AddSession(walletID, txID string) {
	sessionID := GetSessionID(walletID, txID)
	m.sessionsLock.Lock()
	defer m.sessionsLock.Unlock()

	m.activeSessions[sessionID] = time.Now()
	logger.Debug("Session added", "sessionID", sessionID)
}

// CheckDuplicateSession checks for duplicate sessions
func (m *Manager) CheckDuplicateSession(walletID, txID string) bool {
	sessionID := GetSessionID(walletID, txID)

	m.sessionsLock.RLock()
	_, exists := m.activeSessions[sessionID]
	m.sessionsLock.RUnlock()

	if exists {
		logger.Info("Duplicate session detected", "walletID", walletID, "txID", txID)
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
	m.sessionsLock.Lock()
	defer m.sessionsLock.Unlock()

	cleanedCount := 0
	for sessionID, creationTime := range m.activeSessions {
		if now.Sub(creationTime) > m.sessionTimeout {
			delete(m.activeSessions, sessionID)
			cleanedCount++
		}
	}

	if cleanedCount > 0 {
		logger.Info("Cleaned up stale sessions",
			"cleanedCount", cleanedCount,
			"remainingCount", len(m.activeSessions))
	}
}

// GetActiveSessionCount gets the number of active sessions
func (m *Manager) GetActiveSessionCount() int {
	m.sessionsLock.RLock()
	defer m.sessionsLock.RUnlock()
	return len(m.activeSessions)
}

// GetActiveSessions gets all active sessions (for debugging)
func (m *Manager) GetActiveSessions() map[string]time.Time {
	m.sessionsLock.RLock()
	defer m.sessionsLock.RUnlock()
	// return a copy to avoid external modification
	sessions := make(map[string]time.Time)
	maps.Copy(sessions, m.activeSessions)
	return sessions
}
