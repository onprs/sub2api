package service

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type ticketMaintenanceRepositoryStub struct {
	TicketRepository
	started chan struct{}
	once    sync.Once
}

func (r *ticketMaintenanceRepositoryStub) AutoCloseResolvedBatch(ctx context.Context, _ time.Time, _ int) (int, error) {
	r.once.Do(func() { close(r.started) })
	<-ctx.Done()
	return 0, ctx.Err()
}

type ticketMaintenanceAttachmentRepositoryStub struct {
	TicketAttachmentRepository
}

func (r *ticketMaintenanceAttachmentRepositoryStub) ListExpiredForDeletion(ctx context.Context, _ time.Time, _ int) ([]TicketAttachment, error) {
	return nil, ctx.Err()
}

func TestTicketMaintenanceServiceStopCancelsActiveRun(t *testing.T) {
	cfg := &config.Config{}
	cfg.Ticketing.Enabled = true
	tickets := &ticketMaintenanceRepositoryStub{started: make(chan struct{})}
	attachments := &TicketAttachmentService{repo: &ticketMaintenanceAttachmentRepositoryStub{}}
	svc := NewTicketMaintenanceService(tickets, attachments, cfg)
	svc.Start()

	select {
	case <-tickets.started:
	case <-time.After(time.Second):
		t.Fatal("ticket maintenance did not start")
	}

	stopped := make(chan struct{})
	go func() {
		svc.Stop()
		close(stopped)
	}()
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("ticket maintenance did not stop after cancellation")
	}

	require.NotPanics(t, svc.Stop)
}
