// Package protocol defines the private host to container validation protocol.
package protocol

import (
	"time"

	"github.com/nguyen-duc-loc/warp-plus-connector/internal/state"
)

// Version and WARPVersion identify the only tested worker and vendor contracts.
const (
	Version     = 1
	WARPVersion = "2026.8.2100.0"
)

// Request arrives through standard input; its key never belongs in arguments.
type Request struct {
	SessionID string `json:"session_id"`
	Key       string `json:"key"`
}

// Result contains only normalized application evidence.
type Result struct {
	Outcome    state.Outcome   `json:"outcome"`
	ReasonCode string          `json:"reason_code"`
	Evidence   *state.Evidence `json:"evidence,omitempty"`
}

// Receipt excludes the submitted key and vendor credentials.
type Receipt struct {
	Version        int       `json:"version"`
	SessionID      string    `json:"session_id"`
	Phase          string    `json:"phase"`
	RegistrationID string    `json:"registration_id,omitempty"`
	DeviceID       string    `json:"device_id,omitempty"`
	Result         Result    `json:"result"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// Session is the private container journal and contains the input needed to recover.
type Session struct {
	Receipt
	Key string `json:"key"`
}
