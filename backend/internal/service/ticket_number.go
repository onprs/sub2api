package service

import (
	"crypto/rand"
	"fmt"
	"time"
)

const ticketNumberAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"

func NewTicketNumber(now time.Time) (string, error) {
	if now.IsZero() {
		now = time.Now()
	}
	suffix := make([]byte, 6)
	random := make([]byte, len(suffix))
	if _, err := rand.Read(random); err != nil {
		return "", fmt.Errorf("generate ticket number: %w", err)
	}
	for i := range suffix {
		suffix[i] = ticketNumberAlphabet[int(random[i])%len(ticketNumberAlphabet)]
	}
	return fmt.Sprintf("TK-%s-%s", now.Format("20060102"), suffix), nil
}
