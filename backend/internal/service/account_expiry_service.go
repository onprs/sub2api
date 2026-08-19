package service

import (
	"context"
	"log"
	"sync"
	"time"
)

// AccountExpiryService periodically pauses expired accounts and checks rate-limited accounts.
type AccountExpiryService struct {
	accountRepo         AccountRepository
	accountUsageService *AccountUsageService
	interval            time.Duration
	stopCh              chan struct{}
	stopOnce            sync.Once
	wg                  sync.WaitGroup
}

func NewAccountExpiryService(accountRepo AccountRepository, interval time.Duration) *AccountExpiryService {
	return &AccountExpiryService{
		accountRepo: accountRepo,
		interval:    interval,
		stopCh:      make(chan struct{}),
	}
}

func (s *AccountExpiryService) SetAccountUsageService(usageSvc *AccountUsageService) {
	if s != nil {
		s.accountUsageService = usageSvc
	}
}

func (s *AccountExpiryService) Start() {
	if s == nil || s.accountRepo == nil || s.interval <= 0 {
		return
	}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		ticker := time.NewTicker(s.interval)
		defer ticker.Stop()

		s.runOnce()
		for {
			select {
			case <-ticker.C:
				s.runOnce()
			case <-s.stopCh:
				return
			}
		}
	}()
}

func (s *AccountExpiryService) Stop() {
	if s == nil {
		return
	}
	s.stopOnce.Do(func() {
		close(s.stopCh)
	})
	s.wg.Wait()
}

func (s *AccountExpiryService) runOnce() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	updated, err := s.accountRepo.AutoPauseExpiredAccounts(ctx, time.Now())
	if err != nil {
		log.Printf("[AccountExpiry] Auto pause expired accounts failed: %v", err)
	} else if updated > 0 {
		log.Printf("[AccountExpiry] Auto paused %d expired accounts", updated)
	}

	if s.accountUsageService != nil {
		s.accountUsageService.SyncRateLimitedOpenCodeGoAccounts(ctx)
	}
}
