package engine

import (
	"fmt"

	"maunium.net/go/mautrix"
	"maunium.net/go/mautrix/id"
)

// # Config

// ClientConfig holds the per-account parameters needed to construct a Client.
type ClientConfig struct {
	AccountID     string
	HomeserverURL string
	UserID        id.UserID
	AccessToken   string
	DeviceID      id.DeviceID
}

// # Client

// Client wraps a mautrix.Client and exposes a stable internal API.
//
// All engine operations (send, room resolution) are performed through this type.
// Only internal/sync and internal/crypto are permitted to call Underlying() and
// import maunium.net/go/mautrix directly.
type Client struct {
	accountID string
	mx        *mautrix.Client
}

// NewClient creates a Client for the given account configuration.
func NewClient(cfg ClientConfig) (*Client, error) {
	mx, err := mautrix.NewClient(cfg.HomeserverURL, cfg.UserID, cfg.AccessToken)
	if err != nil {
		return nil, fmt.Errorf("create matrix client for account %s: %w", cfg.AccountID, err)
	}
	mx.DeviceID = cfg.DeviceID
	// Use an in-memory state store as the default. The Crypto Manager (Phase 6)
	// will replace this with a durable SQLite-backed implementation.
	mx.StateStore = mautrix.NewMemoryStateStore()
	return &Client{
		accountID: cfg.AccountID,
		mx:        mx,
	}, nil
}

// AccountID returns the opaque account identifier from the daemon configuration.
func (c *Client) AccountID() string { return c.accountID }

// UserID returns the Matrix user ID this client is authenticated as.
func (c *Client) UserID() id.UserID { return c.mx.UserID }

// Underlying returns the underlying mautrix.Client.
// Only internal/sync and internal/crypto should call this method.
func (c *Client) Underlying() *mautrix.Client { return c.mx }
