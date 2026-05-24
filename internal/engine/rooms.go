package engine

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"maunium.net/go/mautrix"
	"maunium.net/go/mautrix/id"
)

// # Room resolution

// ResolveRoom maps a destination string to a Matrix room ID.
//
// Supported prefixes:
//   - !roomid:server  - room ID, returned as-is
//   - #alias:server   - room alias, resolved via the homeserver
//   - @user:server    - user ID, an existing DM room is reused or a new one is created
func (c *Client) ResolveRoom(ctx context.Context, dest string) (id.RoomID, error) {
	switch {
	case strings.HasPrefix(dest, "!"):
		return id.RoomID(dest), nil

	case strings.HasPrefix(dest, "#"):
		resp, err := c.mx.ResolveAlias(ctx, id.RoomAlias(dest))
		if err != nil {
			return "", fmt.Errorf("resolve alias %q: %w", dest, err)
		}
		return resp.RoomID, nil

	case strings.HasPrefix(dest, "@"):
		return c.resolveDMRoom(ctx, id.UserID(dest))

	default:
		return "", fmt.Errorf("destination %q must begin with !, #, or @", dest)
	}
}

// # DM room helpers

// mDirectContent is the schema of the m.direct account data event.
type mDirectContent map[id.UserID][]id.RoomID

// resolveDMRoom returns an existing DM room with the given user, or creates one.
func (c *Client) resolveDMRoom(ctx context.Context, userID id.UserID) (id.RoomID, error) {
	existing, err := c.findDMRoom(ctx, userID)
	if err != nil {
		return "", err
	}
	if existing != "" {
		return existing, nil
	}
	return c.createDMRoom(ctx, userID)
}

// findDMRoom checks the m.direct account data for an existing DM room.
// Returns "" with a nil error when no DM room exists yet.
func (c *Client) findDMRoom(ctx context.Context, userID id.UserID) (id.RoomID, error) {
	var direct mDirectContent
	err := c.mx.GetAccountData(ctx, "m.direct", &direct)
	if err != nil {
		// M_NOT_FOUND simply means no m.direct data has been written yet.
		if errors.Is(err, mautrix.MNotFound) {
			return "", nil
		}
		return "", fmt.Errorf("get m.direct for account %s: %w", c.accountID, err)
	}
	rooms := direct[userID]
	if len(rooms) == 0 {
		return "", nil
	}
	return rooms[0], nil
}

// createDMRoom creates a DM room with userID as invitee and records it in
// the m.direct account data so future calls find the same room.
func (c *Client) createDMRoom(ctx context.Context, userID id.UserID) (id.RoomID, error) {
	resp, err := c.mx.CreateRoom(ctx, &mautrix.ReqCreateRoom{
		Invite:   []id.UserID{userID},
		IsDirect: true,
	})
	if err != nil {
		return "", fmt.Errorf("create DM room with %s: %w", userID, err)
	}
	roomID := resp.RoomID

	// Best-effort: update m.direct so future lookups find this room.
	var direct mDirectContent
	if getErr := c.mx.GetAccountData(ctx, "m.direct", &direct); getErr != nil && !errors.Is(getErr, mautrix.MNotFound) {
		slog.Warn("engine: could not read m.direct before updating it",
			"account_id", c.accountID,
			"error", getErr,
		)
	}
	if direct == nil {
		direct = make(mDirectContent)
	}
	direct[userID] = append(direct[userID], roomID)
	if setErr := c.mx.SetAccountData(ctx, "m.direct", direct); setErr != nil {
		slog.Warn("engine: could not update m.direct after creating DM room",
			"account_id", c.accountID,
			"room_id", roomID,
			"error", setErr,
		)
	}

	return roomID, nil
}
