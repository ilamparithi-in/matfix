package account

import (
	"errors"
	"fmt"
	"strings"
)

// ErrAllAccountsFailed is returned by AccountManager.StartAll when every
// configured account fails to initialise. The relay cannot serve any requests
// in this state and the process should terminate.
var ErrAllAccountsFailed = errors.New("account: all accounts failed to start")

// checkFailures inspects the set of account contexts and returns
// ErrAllAccountsFailed when every account is in StatusFailed.
//
// Partial failure (at least one account available) is acceptable: the relay
// continues operating with the healthy accounts and returns nil.
// An empty accounts map is also treated as successful (no-op startup).
func checkFailures(accounts map[string]*AccountContext) error {
	if len(accounts) == 0 {
		return nil
	}

	var reasons []string
	for id, actx := range accounts {
		if actx.IsAvailable() {
			// At least one operational account — relay can continue.
			return nil
		}
		reasons = append(reasons, fmt.Sprintf("%s: %v", id, actx.err))
	}
	return fmt.Errorf("%w: %s", ErrAllAccountsFailed, strings.Join(reasons, "; "))
}
