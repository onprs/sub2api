package service

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

var (
	ErrOpenCodeGoConsoleAuthTicketNotFound = errors.New("opencode go console auth ticket not found")
	ErrOpenCodeGoConsoleAuthTicketExpired  = errors.New("opencode go console auth ticket expired")
	ErrOpenCodeGoConsoleAuthTicketUsed     = errors.New("opencode go console auth ticket used")
	ErrOpenCodeGoConsoleAuthTicketMismatch = errors.New("opencode go console auth ticket mismatch")
)

type OpenCodeGoConsoleAuthTicket struct {
	ID          string    `json:"ticket_id"`
	AccountID   int64     `json:"account_id"`
	WorkspaceID string    `json:"workspace_id"`
	ExpiresAt   time.Time `json:"expires_at"`
	Used        bool      `json:"-"`
}

type OpenCodeGoConsoleAuthTicketStore struct {
	mu      sync.Mutex
	ttl     time.Duration
	tickets map[string]*OpenCodeGoConsoleAuthTicket
	now     func() time.Time
}

func NewOpenCodeGoConsoleAuthTicketStore(ttl time.Duration) *OpenCodeGoConsoleAuthTicketStore {
	if ttl <= 0 {
		ttl = 10 * time.Minute
	}
	return &OpenCodeGoConsoleAuthTicketStore{
		ttl:     ttl,
		tickets: make(map[string]*OpenCodeGoConsoleAuthTicket),
		now:     time.Now,
	}
}

func (s *OpenCodeGoConsoleAuthTicketStore) Create(accountID int64, workspaceID string) (*OpenCodeGoConsoleAuthTicket, error) {
	if s == nil {
		return nil, fmt.Errorf("opencode go console auth ticket store is nil")
	}
	workspaceID = strings.TrimSpace(workspaceID)
	if accountID <= 0 {
		return nil, fmt.Errorf("account id is required")
	}
	if workspaceID == "" {
		return nil, fmt.Errorf("workspace id is required")
	}

	id, err := randomOpenCodeGoConsoleTicketID()
	if err != nil {
		return nil, err
	}
	now := s.now().UTC()
	ticket := &OpenCodeGoConsoleAuthTicket{
		ID:          id,
		AccountID:   accountID,
		WorkspaceID: workspaceID,
		ExpiresAt:   now.Add(s.ttl),
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.deleteExpiredLocked(now)
	s.tickets[id] = ticket
	return cloneOpenCodeGoConsoleAuthTicket(ticket), nil
}

func (s *OpenCodeGoConsoleAuthTicketStore) Peek(ticketID string) (*OpenCodeGoConsoleAuthTicket, error) {
	if s == nil {
		return nil, ErrOpenCodeGoConsoleAuthTicketNotFound
	}
	ticketID = strings.TrimSpace(ticketID)
	if ticketID == "" {
		return nil, ErrOpenCodeGoConsoleAuthTicketNotFound
	}
	now := s.now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	ticket, err := s.getUsableLocked(ticketID, now)
	if err != nil {
		return nil, err
	}
	return cloneOpenCodeGoConsoleAuthTicket(ticket), nil
}

func (s *OpenCodeGoConsoleAuthTicketStore) ConsumeForAccount(ticketID string, accountID int64, workspaceID string) (*OpenCodeGoConsoleAuthTicket, error) {
	return s.consume(ticketID, accountID, workspaceID, true)
}

func (s *OpenCodeGoConsoleAuthTicketStore) ConsumeForWorkspace(ticketID, workspaceID string) (*OpenCodeGoConsoleAuthTicket, error) {
	return s.consume(ticketID, 0, workspaceID, false)
}

func (s *OpenCodeGoConsoleAuthTicketStore) ConsumeWithObservedWorkspace(ticketID, workspaceID string) (*OpenCodeGoConsoleAuthTicket, error) {
	if s == nil {
		return nil, ErrOpenCodeGoConsoleAuthTicketNotFound
	}
	ticketID = strings.TrimSpace(ticketID)
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return nil, ErrOpenCodeGoConsoleAuthTicketMismatch
	}
	now := s.now().UTC()

	s.mu.Lock()
	defer s.mu.Unlock()
	ticket, err := s.getUsableLocked(ticketID, now)
	if err != nil {
		return nil, err
	}
	ticket.Used = true
	consumed := cloneOpenCodeGoConsoleAuthTicket(ticket)
	consumed.WorkspaceID = workspaceID
	return consumed, nil
}

func (s *OpenCodeGoConsoleAuthTicketStore) consume(ticketID string, accountID int64, workspaceID string, checkAccount bool) (*OpenCodeGoConsoleAuthTicket, error) {
	if s == nil {
		return nil, ErrOpenCodeGoConsoleAuthTicketNotFound
	}
	ticketID = strings.TrimSpace(ticketID)
	workspaceID = strings.TrimSpace(workspaceID)
	now := s.now().UTC()

	s.mu.Lock()
	defer s.mu.Unlock()
	ticket, err := s.getUsableLocked(ticketID, now)
	if err != nil {
		return nil, err
	}
	if checkAccount && ticket.AccountID != accountID {
		return nil, ErrOpenCodeGoConsoleAuthTicketMismatch
	}
	if ticket.WorkspaceID != workspaceID {
		return nil, ErrOpenCodeGoConsoleAuthTicketMismatch
	}
	ticket.Used = true
	return cloneOpenCodeGoConsoleAuthTicket(ticket), nil
}

func (s *OpenCodeGoConsoleAuthTicketStore) getUsableLocked(ticketID string, now time.Time) (*OpenCodeGoConsoleAuthTicket, error) {
	ticket, ok := s.tickets[ticketID]
	if !ok {
		return nil, ErrOpenCodeGoConsoleAuthTicketNotFound
	}
	if ticket.Used {
		return nil, ErrOpenCodeGoConsoleAuthTicketUsed
	}
	if !now.Before(ticket.ExpiresAt) {
		delete(s.tickets, ticketID)
		return nil, ErrOpenCodeGoConsoleAuthTicketExpired
	}
	return ticket, nil
}

func (s *OpenCodeGoConsoleAuthTicketStore) deleteExpiredLocked(now time.Time) {
	for id, ticket := range s.tickets {
		if ticket == nil || !now.Before(ticket.ExpiresAt) {
			delete(s.tickets, id)
		}
	}
}

func randomOpenCodeGoConsoleTicketID() (string, error) {
	var buf [32]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf[:]), nil
}

func cloneOpenCodeGoConsoleAuthTicket(ticket *OpenCodeGoConsoleAuthTicket) *OpenCodeGoConsoleAuthTicket {
	if ticket == nil {
		return nil
	}
	copied := *ticket
	return &copied
}
