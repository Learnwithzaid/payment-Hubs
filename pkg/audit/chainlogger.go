package audit

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"time"
)

// LogEntry represents a single audit log entry
type LogEntry struct {
	Timestamp    string `json:"timestamp"`
	PreviousHash string `json:"previous_hash"`
	Payload      string `json:"payload"`
	Hash         string `json:"hash"`
	Signature    string `json:"signature,omitempty"`
}

// ChainLogger provides a tamper-proof logging mechanism using hash chaining.
type ChainLogger struct {
	mu           sync.Mutex
	previousHash string
}

// NewChainLogger creates a new ChainLogger initialized with a zero hash.
func NewChainLogger() *ChainLogger {
	return &ChainLogger{
		previousHash: strings.Repeat("0", 64),
	}
}

// Append adds a new log entry to the chain.
func (c *ChainLogger) Append(payload string) *LogEntry {
	c.mu.Lock()
	defer c.mu.Unlock()

	entry := &LogEntry{
		Timestamp:    time.Now().UTC().Format(time.RFC3339),
		PreviousHash: c.previousHash,
		Payload:      payload,
	}

	hashInput := fmt.Sprintf("%s|%s|%s", entry.PreviousHash, entry.Timestamp, entry.Payload)
	hash := sha256.Sum256([]byte(hashInput))
	entry.Hash = hex.EncodeToString(hash[:])

	c.previousHash = entry.Hash
	return entry
}

// VerifyChain checks if a slice of entries forms a valid hash chain.
func VerifyChain(entries []*LogEntry) bool {
	if len(entries) == 0 {
		return true
	}

	for i, entry := range entries {
		var prevHash string
		if i == 0 {
			prevHash = entry.PreviousHash
		} else {
			prevHash = entries[i-1].Hash
			if entry.PreviousHash != prevHash {
				return false
			}
		}

		hashInput := fmt.Sprintf("%s|%s|%s", prevHash, entry.Timestamp, entry.Payload)
		hash := sha256.Sum256([]byte(hashInput))
		computedHash := hex.EncodeToString(hash[:])

		if computedHash != entry.Hash {
			return false
		}
	}
	return true
}
