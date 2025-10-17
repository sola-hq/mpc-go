package session

import (
	"testing"
	"time"
)

func TestManager_AddSession(t *testing.T) {
	manager := NewManager(1*time.Minute, 5*time.Minute)

	// Test adding a session
	manager.AddSession("wallet1", "tx1")

	if manager.GetActiveSessionCount() != 1 {
		t.Errorf("Expected 1 active session, got %d", manager.GetActiveSessionCount())
	}

	// Test adding another session
	manager.AddSession("wallet2", "tx2")

	if manager.GetActiveSessionCount() != 2 {
		t.Errorf("Expected 2 active sessions, got %d", manager.GetActiveSessionCount())
	}
}

func TestManager_CheckDuplicateSession(t *testing.T) {
	manager := NewManager(1*time.Minute, 5*time.Minute)

	// Test non-duplicate session
	if manager.HasSession("wallet1", "tx1") {
		t.Error("Expected non-duplicate session to return false")
	}

	// Add session
	manager.AddSession("wallet1", "tx1")

	// Test duplicate session
	if !manager.HasSession("wallet1", "tx1") {
		t.Error("Expected duplicate session to return true")
	}

	// Test different wallet/tx combination
	if manager.HasSession("wallet1", "tx2") {
		t.Error("Expected different tx to return false")
	}

	if manager.HasSession("wallet2", "tx1") {
		t.Error("Expected different wallet to return false")
	}
}

func TestManager_CleanupStaleSessions(t *testing.T) {
	manager := NewManager(1*time.Minute, 1*time.Millisecond) // Very short timeout

	// Add a session
	manager.AddSession("wallet1", "tx1")

	if manager.GetActiveSessionCount() != 1 {
		t.Errorf("Expected 1 active session, got %d", manager.GetActiveSessionCount())
	}

	// Wait for session to become stale
	time.Sleep(10 * time.Millisecond)

	// Manually trigger cleanup
	manager.cleanupStaleSessions()

	if manager.GetActiveSessionCount() != 0 {
		t.Errorf("Expected 0 active sessions after cleanup, got %d", manager.GetActiveSessionCount())
	}
}

func TestManager_GetActiveSessions(t *testing.T) {
	manager := NewManager(1*time.Minute, 5*time.Minute)

	// Add sessions
	manager.AddSession("wallet1", "tx1")
	manager.AddSession("wallet2", "tx2")

	sessions := manager.GetActiveSessions()

	if len(sessions) != 2 {
		t.Errorf("Expected 2 active sessions, got %d", len(sessions))
	}

	// Verify session IDs
	expectedSession1 := "wallet1-tx1"
	expectedSession2 := "wallet2-tx2"

	if _, exists := sessions[expectedSession1]; !exists {
		t.Errorf("Expected session %s to exist", expectedSession1)
	}

	if _, exists := sessions[expectedSession2]; !exists {
		t.Errorf("Expected session %s to exist", expectedSession2)
	}
}
