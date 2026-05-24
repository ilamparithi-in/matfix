package account

import (
	"errors"
	"testing"
)

// # checkFailures tests

func TestCheckFailures_AllFailed(t *testing.T) {
	accounts := map[string]*AccountContext{
		"a1": {accountID: "a1", status: StatusFailed, err: errors.New("connection refused")},
		"a2": {accountID: "a2", status: StatusFailed, err: errors.New("auth failed")},
	}
	err := checkFailures(accounts)
	if err == nil {
		t.Fatal("expected error when all accounts fail, got nil")
	}
	if !errors.Is(err, ErrAllAccountsFailed) {
		t.Fatalf("expected ErrAllAccountsFailed, got: %v", err)
	}
}

func TestCheckFailures_PartialFailure(t *testing.T) {
	accounts := map[string]*AccountContext{
		"a1": {accountID: "a1", status: StatusFailed, err: errors.New("connection refused")},
		"a2": {accountID: "a2", status: StatusAvailable},
	}
	if err := checkFailures(accounts); err != nil {
		t.Fatalf("expected nil for partial failure (relay should continue), got: %v", err)
	}
}

func TestCheckFailures_Empty(t *testing.T) {
	if err := checkFailures(nil); err != nil {
		t.Fatalf("expected nil for empty account map, got: %v", err)
	}
	if err := checkFailures(map[string]*AccountContext{}); err != nil {
		t.Fatalf("expected nil for empty account map, got: %v", err)
	}
}

func TestCheckFailures_AllAvailable(t *testing.T) {
	accounts := map[string]*AccountContext{
		"a1": {accountID: "a1", status: StatusAvailable},
		"a2": {accountID: "a2", status: StatusAvailable},
	}
	if err := checkFailures(accounts); err != nil {
		t.Fatalf("expected nil when all accounts available, got: %v", err)
	}
}

// # AccountContext tests

func TestAccountContext_IsAvailable(t *testing.T) {
	tests := []struct {
		name   string
		status AccountStatus
		want   bool
	}{
		{"initializing", StatusInitializing, false},
		{"available", StatusAvailable, true},
		{"failed", StatusFailed, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := &AccountContext{status: tt.status}
			if got := a.IsAvailable(); got != tt.want {
				t.Errorf("IsAvailable() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAccountContext_Accessors(t *testing.T) {
	sentinel := errors.New("startup error")
	a := &AccountContext{
		accountID: "bot1",
		status:    StatusFailed,
		err:       sentinel,
	}
	if a.AccountID() != "bot1" {
		t.Errorf("AccountID() = %q, want %q", a.AccountID(), "bot1")
	}
	if a.Err() != sentinel {
		t.Errorf("Err() = %v, want %v", a.Err(), sentinel)
	}
	if a.Client() != nil {
		t.Error("expected nil Client on failed account")
	}
	if a.SyncManager() != nil {
		t.Error("expected nil SyncManager on failed account")
	}
	if a.CryptoManager() != nil {
		t.Error("expected nil CryptoManager on failed account")
	}
}

// # AccountManager unit tests (no I/O, pre-seeded maps)

func newManagerWithAccounts(contexts ...*AccountContext) *AccountManager {
	m := &AccountManager{
		accounts: make(map[string]*AccountContext),
	}
	for _, a := range contexts {
		m.accounts[a.accountID] = a
	}
	return m
}

func TestAccountManager_Get(t *testing.T) {
	avail := &AccountContext{accountID: "a1", status: StatusAvailable}
	m := newManagerWithAccounts(avail)

	if got := m.Get("a1"); got != avail {
		t.Errorf("Get(a1) = %v, want %v", got, avail)
	}
	if got := m.Get("missing"); got != nil {
		t.Errorf("Get(missing) = %v, want nil", got)
	}
}

func TestAccountManager_Available(t *testing.T) {
	a1 := &AccountContext{accountID: "a1", status: StatusAvailable}
	a2 := &AccountContext{accountID: "a2", status: StatusFailed, err: errors.New("down")}
	a3 := &AccountContext{accountID: "a3", status: StatusAvailable}

	m := newManagerWithAccounts(a1, a2, a3)
	avail := m.Available()

	if len(avail) != 2 {
		t.Fatalf("Available() returned %d accounts, want 2", len(avail))
	}
	for _, a := range avail {
		if !a.IsAvailable() {
			t.Errorf("Available() returned non-available account %s", a.AccountID())
		}
	}
}

func TestAccountManager_All(t *testing.T) {
	a1 := &AccountContext{accountID: "a1", status: StatusAvailable}
	a2 := &AccountContext{accountID: "a2", status: StatusFailed, err: errors.New("down")}

	m := newManagerWithAccounts(a1, a2)
	all := m.All()

	if len(all) != 2 {
		t.Fatalf("All() returned %d accounts, want 2", len(all))
	}
}
