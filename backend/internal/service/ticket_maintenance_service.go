package service

import (
	"context"
	"database/sql"
	"log/slog"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/google/uuid"
)

const (
	ticketMaintenanceLeaderLockKey = "ticket:maintenance:leader"
	ticketMaintenanceLeaderLockTTL = 5 * time.Minute
	ticketMaintenanceTimeout       = 3 * time.Minute
)

type TicketMaintenanceService struct {
	tickets     TicketRepository
	attachments *TicketAttachmentService
	cfg         *config.Config
	interval    time.Duration
	lockCache   LeaderLockCache
	db          *sql.DB
	instanceID  string
	runCtx      context.Context
	cancel      context.CancelFunc
	startOnce   sync.Once
	stopOnce    sync.Once
	wg          sync.WaitGroup
}

func NewTicketMaintenanceService(tickets TicketRepository, attachments *TicketAttachmentService, cfg *config.Config) *TicketMaintenanceService {
	runCtx, cancel := context.WithCancel(context.Background())
	return &TicketMaintenanceService{
		tickets: tickets, attachments: attachments, cfg: cfg, interval: time.Hour,
		instanceID: uuid.NewString(), runCtx: runCtx, cancel: cancel,
	}
}

func (s *TicketMaintenanceService) SetLeaderLock(lockCache LeaderLockCache, db *sql.DB) {
	if s == nil {
		return
	}
	s.lockCache = lockCache
	s.db = db
}

func (s *TicketMaintenanceService) Start() {
	if s == nil || s.tickets == nil || s.attachments == nil || (s.cfg != nil && !s.cfg.Ticketing.Enabled) {
		return
	}
	s.startOnce.Do(func() {
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			ticker := time.NewTicker(s.interval)
			defer ticker.Stop()
			s.runOnce(s.runCtx)
			for {
				select {
				case <-ticker.C:
					s.runOnce(s.runCtx)
				case <-s.runCtx.Done():
					return
				}
			}
		}()
	})
}

func (s *TicketMaintenanceService) Stop() {
	if s == nil {
		return
	}
	s.stopOnce.Do(s.cancel)
	s.wg.Wait()
}

func (s *TicketMaintenanceService) runOnce(ctx context.Context) {
	lockCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	release, ok := tryAcquireSingletonLeaderLock(lockCtx, s.lockCache, s.db, ticketMaintenanceLeaderLockKey, s.instanceID, ticketMaintenanceLeaderLockTTL)
	cancel()
	if !ok {
		return
	}
	defer release()

	jobCtx, cancel := context.WithTimeout(ctx, ticketMaintenanceTimeout)
	defer cancel()
	closed, err := s.tickets.AutoCloseResolvedBatch(jobCtx, time.Now(), 100)
	if err != nil {
		slog.Error("ticket auto-close maintenance failed", "error", err)
	} else if closed > 0 {
		slog.Info("ticket auto-close maintenance completed", "closed", closed)
	}
	batchSize := 100
	if s.cfg != nil && s.cfg.Ticketing.Attachments.CleanupBatchSize > 0 {
		batchSize = s.cfg.Ticketing.Attachments.CleanupBatchSize
	}
	deleted, err := s.attachments.CleanupExpired(jobCtx, time.Now(), batchSize)
	if err != nil {
		slog.Error("ticket attachment cleanup failed", "error", err)
	} else if deleted > 0 {
		slog.Info("ticket attachment cleanup completed", "deleted", deleted)
	}
}
