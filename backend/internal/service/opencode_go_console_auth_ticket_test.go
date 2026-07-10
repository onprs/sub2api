package service

import (
	"errors"
	"testing"
	"time"
)

func TestOpenCodeGoConsoleAuthTicketStoreConsumesTicketOnce(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 22, 5, 0, 0, 0, time.UTC)
	store := NewOpenCodeGoConsoleAuthTicketStore(10 * time.Minute)
	store.now = func() time.Time { return now }

	ticket, err := store.Create(123, "wrk_01KVD1MSEZ20TGGWZTPS4M9TCZ")
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if ticket.ID == "" || !ticket.ExpiresAt.Equal(now.Add(10*time.Minute)) {
		t.Fatalf("unexpected ticket: %#v", ticket)
	}

	consumed, err := store.ConsumeForAccount(ticket.ID, 123, "wrk_01KVD1MSEZ20TGGWZTPS4M9TCZ")
	if err != nil {
		t.Fatalf("ConsumeForAccount() error = %v", err)
	}
	if consumed.AccountID != 123 || consumed.WorkspaceID != "wrk_01KVD1MSEZ20TGGWZTPS4M9TCZ" {
		t.Fatalf("unexpected consumed ticket: %#v", consumed)
	}

	_, err = store.ConsumeForAccount(ticket.ID, 123, "wrk_01KVD1MSEZ20TGGWZTPS4M9TCZ")
	if !errors.Is(err, ErrOpenCodeGoConsoleAuthTicketUsed) {
		t.Fatalf("second consume error = %v, want used", err)
	}
}

func TestOpenCodeGoConsoleAuthTicketStoreRejectsExpiredAndMismatchedTicket(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 22, 5, 0, 0, 0, time.UTC)
	store := NewOpenCodeGoConsoleAuthTicketStore(10 * time.Minute)
	store.now = func() time.Time { return now }

	ticket, err := store.Create(123, "wrk_01KVD1MSEZ20TGGWZTPS4M9TCZ")
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if _, err = store.ConsumeForAccount(ticket.ID, 456, "wrk_01KVD1MSEZ20TGGWZTPS4M9TCZ"); !errors.Is(err, ErrOpenCodeGoConsoleAuthTicketMismatch) {
		t.Fatalf("account mismatch error = %v", err)
	}
	if _, err = store.ConsumeForAccount(ticket.ID, 123, "wrk_other"); !errors.Is(err, ErrOpenCodeGoConsoleAuthTicketMismatch) {
		t.Fatalf("workspace mismatch error = %v", err)
	}

	store.now = func() time.Time { return now.Add(11 * time.Minute) }
	if _, err = store.ConsumeForAccount(ticket.ID, 123, "wrk_01KVD1MSEZ20TGGWZTPS4M9TCZ"); !errors.Is(err, ErrOpenCodeGoConsoleAuthTicketExpired) {
		t.Fatalf("expired error = %v", err)
	}
}

func TestOpenCodeGoConsoleAuthTicketStoreConsumesObservedWorkspace(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 22, 5, 0, 0, 0, time.UTC)
	store := NewOpenCodeGoConsoleAuthTicketStore(10 * time.Minute)
	store.now = func() time.Time { return now }

	ticket, err := store.Create(123, "wrk_requested")
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	consumed, err := store.ConsumeWithObservedWorkspace(ticket.ID, "wrk_observed")
	if err != nil {
		t.Fatalf("ConsumeWithObservedWorkspace() error = %v", err)
	}
	if consumed.AccountID != 123 {
		t.Fatalf("account = %d", consumed.AccountID)
	}
	if consumed.WorkspaceID != "wrk_observed" {
		t.Fatalf("workspace = %q, want observed workspace", consumed.WorkspaceID)
	}

	_, err = store.ConsumeWithObservedWorkspace(ticket.ID, "wrk_observed")
	if !errors.Is(err, ErrOpenCodeGoConsoleAuthTicketUsed) {
		t.Fatalf("second consume error = %v, want used", err)
	}
}
