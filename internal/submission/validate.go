package submission

import (
	"errors"
	"fmt"
	"strings"
)

// # Sentinel errors

var (
	// ErrUnknownAccount is returned when the requested account is not configured.
	ErrUnknownAccount = errors.New("submission: unknown account")

	// ErrInvalidDestination is returned when the destination string has no valid Matrix sigil.
	ErrInvalidDestination = errors.New("submission: invalid destination")

	// ErrNilMessage is returned when SubmitRequest.Message is nil.
	ErrNilMessage = errors.New("submission: message must not be nil")
)

// validateRequest checks the structural validity of req before any DB or network call.
func validateRequest(req SubmitRequest, knownAccounts map[string]struct{}) error {
	if _, ok := knownAccounts[req.AccountID]; !ok {
		return fmt.Errorf("%w: %q", ErrUnknownAccount, req.AccountID)
	}
	if err := validateDestination(req.Destination); err != nil {
		return err
	}
	if req.Message == nil {
		return ErrNilMessage
	}
	return nil
}

// validateDestination checks that dest carries a recognised Matrix sigil.
// It performs a format-only check; actual room resolution happens later.
func validateDestination(dest string) error {
	if strings.HasPrefix(dest, "!") || strings.HasPrefix(dest, "#") || strings.HasPrefix(dest, "@") {
		return nil
	}
	return fmt.Errorf("%w: %q must begin with !, # or @", ErrInvalidDestination, dest)
}
