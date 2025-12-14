package audit

import (
	"testing"
)

func TestChainLogger(t *testing.T) {
	logger := NewChainLogger()

	e1 := logger.Append("action: login, user: alice")
	e2 := logger.Append("action: read, resource: doc1")
	e3 := logger.Append("action: logout, user: alice")

	// Verify chain integrity
	chain := []*LogEntry{e1, e2, e3}
	if !VerifyChain(chain) {
		t.Error("VerifyChain failed for valid chain")
	}

	// Tamper with e2 payload
	originalPayload := e2.Payload
	e2.Payload = "action: delete, resource: doc1"
	if VerifyChain(chain) {
		t.Error("VerifyChain succeeded for tampered payload")
	}

	// Restore payload, tamper with hash
	e2.Payload = originalPayload
	originalHash := e2.Hash
	e2.Hash = "deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef"
	if VerifyChain(chain) {
		t.Error("VerifyChain succeeded for tampered hash")
	}

    // Restore hash
    e2.Hash = originalHash
    
    // Tamper with e3 previous hash
    e3.PreviousHash = "deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef"
    if VerifyChain(chain) {
        t.Error("VerifyChain succeeded for broken link")
    }
}
