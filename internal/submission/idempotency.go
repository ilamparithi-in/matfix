package submission

import (
	"context"
	"fmt"

	"github.com/ilamparithi-in/matfix/internal/persistence"
)

// checkIdempotency looks up key in the queue store for accountID.
//
// Returns the existing job ID when a matching record is found, allowing Submit
// to return early without creating a duplicate job.
// Returns ("", nil) when key is empty or no matching record exists.
func checkIdempotency(ctx context.Context, store persistence.QueueStore, accountID, key string) (string, error) {
	if key == "" {
		return "", nil
	}
	entry, err := store.GetByIdempotencyKey(ctx, accountID, key)
	if err != nil {
		return "", fmt.Errorf("submission: idempotency check: %w", err)
	}
	if entry != nil {
		return entry.ID, nil
	}
	return "", nil
}
