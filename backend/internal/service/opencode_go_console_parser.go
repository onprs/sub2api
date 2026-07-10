package service

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	OpenCodeGoConsoleAuthStatusReady   = "ready"
	OpenCodeGoConsoleAuthStatusExpired = "expired"
	OpenCodeGoConsoleAuthStatusError   = "error"

	openCodeGoUsageSourceOfficialConsole = "official_console"
	openCodeGoUsageSourceOfficialLabel   = "OpenCode official Console"
)

// OpenCodeGoConsoleSummary is the parsed official Console snapshot for one Go workspace.
type OpenCodeGoConsoleSummary struct {
	WorkspaceID string
	FetchedAt   time.Time
	Usage       OpenCodeGoConsoleUsage
	Referral    OpenCodeGoReferralSummary
}

type OpenCodeGoConsoleUsage struct {
	FiveHour  OpenCodeGoUsageWindow
	SevenDay  OpenCodeGoUsageWindow
	ThirtyDay OpenCodeGoUsageWindow
}

type OpenCodeGoUsageWindow struct {
	Status       string
	UsagePercent float64
	ResetInSec   int
	ResetsAt     *time.Time
}

type OpenCodeGoReferralSummary struct {
	ReferralCode         string
	HasReferral          bool
	RewardAmountCents    int
	Rewards              []OpenCodeGoReferralReward
	AvailableCount       int
	AvailableAmountCents int
	AppliedCount         int
}

type OpenCodeGoReferralReward struct {
	ID          string     `json:"id"`
	Source      string     `json:"source"`
	Status      string     `json:"status"`
	Email       string     `json:"-"`
	MaskedEmail string     `json:"masked_email,omitempty"`
	AmountCents int        `json:"amount_cents"`
	TimeCreated *time.Time `json:"time_created,omitempty"`
	TimeApplied *time.Time `json:"time_applied,omitempty"`
}

type OpenCodeGoServerActions struct {
	ReferralUsagePreview string
	ReferralRewardApply  string
	LiteSubscriptionGet  string
}

var (
	opencodeGoWorkspaceIDPatterns = []*regexp.Regexp{
		regexp.MustCompile(`lite\.subscription\.get\[\\"(wrk_[A-Za-z0-9]+)\\"\]`),
		regexp.MustCompile(`lite\.subscription\.get\["(wrk_[A-Za-z0-9]+)"\]`),
		regexp.MustCompile(`/workspace/(wrk_[A-Za-z0-9]+)/go`),
	}
	opencodeGoUsageWindowPatterns = map[string]*regexp.Regexp{
		"rolling": regexp.MustCompile(`rollingUsage:[^=]*=\{status:"([^"]+)",resetInSec:(\d+),usagePercent:(\d+(?:\.\d+)?)\}`),
		"weekly":  regexp.MustCompile(`weeklyUsage:[^=]*=\{status:"([^"]+)",resetInSec:(\d+),usagePercent:(\d+(?:\.\d+)?)\}`),
		"monthly": regexp.MustCompile(`monthlyUsage:[^=]*=\{status:"([^"]+)",resetInSec:(\d+),usagePercent:(\d+(?:\.\d+)?)\}`),
	}
	opencodeGoReferralRewardPattern  = regexp.MustCompile(`\{id:"([^"]+)",source:"([^"]*)",status:"([^"]*)",email:"([^"]*)",amount:(\d+),timeCreated:[^=]*=new Date\("([^"]+)"\),timeApplied:(null|new Date\("[^"]+"\)|"[^"]*"|[^}\]]+)`)
	opencodeGoServerRefConstPattern  = regexp.MustCompile(`const\s+([A-Za-z0-9_$]+)\s*=\s*createServerReference\("([a-f0-9]{32,})"\)`)
	opencodeGoServerRefUsePattern    = regexp.MustCompile(`(?:query|action)\(\s*([A-Za-z0-9_$]+)\s*,\s*"([^"]+)"\s*\)`)
	opencodeGoServerRefDirectPattern = regexp.MustCompile(`(?:query|action)\(\s*createServerReference\("([a-f0-9]{32,})"\)\s*,\s*"([^"]+)"\s*\)`)
)

func ParseOpenCodeGoConsolePage(html string, fetchedAt time.Time) (*OpenCodeGoConsoleSummary, error) {
	if strings.TrimSpace(html) == "" {
		return nil, fmt.Errorf("opencode go console page is empty")
	}

	workspaceID := parseOpenCodeGoWorkspaceID(html)
	if workspaceID == "" {
		return nil, fmt.Errorf("opencode go console workspace id not found")
	}

	fetchedAt = fetchedAt.UTC()
	rolling, err := parseOpenCodeGoUsageWindow(html, "rolling", fetchedAt)
	if err != nil {
		return nil, err
	}
	weekly, err := parseOpenCodeGoUsageWindow(html, "weekly", fetchedAt)
	if err != nil {
		return nil, err
	}
	monthly, err := parseOpenCodeGoUsageWindow(html, "monthly", fetchedAt)
	if err != nil {
		return nil, err
	}

	referral := parseOpenCodeGoReferralSummary(html)

	return &OpenCodeGoConsoleSummary{
		WorkspaceID: workspaceID,
		FetchedAt:   fetchedAt,
		Usage: OpenCodeGoConsoleUsage{
			FiveHour:  rolling,
			SevenDay:  weekly,
			ThirtyDay: monthly,
		},
		Referral: referral,
	}, nil
}

func parseOpenCodeGoWorkspaceID(html string) string {
	for _, pattern := range opencodeGoWorkspaceIDPatterns {
		if match := pattern.FindStringSubmatch(html); len(match) == 2 {
			return match[1]
		}
	}
	return ""
}

func parseOpenCodeGoUsageWindow(html, name string, fetchedAt time.Time) (OpenCodeGoUsageWindow, error) {
	pattern := opencodeGoUsageWindowPatterns[name]
	if pattern == nil {
		return OpenCodeGoUsageWindow{}, fmt.Errorf("unknown opencode go usage window %q", name)
	}
	match := pattern.FindStringSubmatch(html)
	if len(match) != 4 {
		return OpenCodeGoUsageWindow{}, fmt.Errorf("opencode go %s usage not found", name)
	}
	resetInSec, err := strconv.Atoi(match[2])
	if err != nil {
		return OpenCodeGoUsageWindow{}, fmt.Errorf("parse opencode go %s reset seconds: %w", name, err)
	}
	percent, err := strconv.ParseFloat(match[3], 64)
	if err != nil {
		return OpenCodeGoUsageWindow{}, fmt.Errorf("parse opencode go %s usage percent: %w", name, err)
	}
	resetsAt := fetchedAt.Add(time.Duration(resetInSec) * time.Second)
	return OpenCodeGoUsageWindow{
		Status:       match[1],
		UsagePercent: percent,
		ResetInSec:   resetInSec,
		ResetsAt:     &resetsAt,
	}, nil
}

func parseOpenCodeGoReferralSummary(html string) OpenCodeGoReferralSummary {
	summary := OpenCodeGoReferralSummary{}
	if code := firstStringSubmatch(html, `referralCode:"([^"]*)"`); code != "" {
		summary.ReferralCode = code
	}
	summary.HasReferral = strings.Contains(html, "hasReferral:!0") || strings.Contains(html, "hasReferral:true")
	summary.RewardAmountCents = firstIntSubmatch(html, `rewardAmount:(\d+)`)

	for _, match := range opencodeGoReferralRewardPattern.FindAllStringSubmatch(html, -1) {
		if len(match) < 8 {
			continue
		}
		amount, _ := strconv.Atoi(match[5])
		createdAt := parseOpenCodeGoConsoleDate(match[6])
		appliedAt := parseOpenCodeGoConsoleDateExpression(match[7])
		reward := OpenCodeGoReferralReward{
			ID:          match[1],
			Source:      match[2],
			Status:      match[3],
			MaskedEmail: MaskOpenCodeGoReferralEmail(match[4]),
			AmountCents: amount,
			TimeCreated: createdAt,
			TimeApplied: appliedAt,
		}
		summary.Rewards = append(summary.Rewards, reward)
		switch reward.Status {
		case "available":
			summary.AvailableCount++
			summary.AvailableAmountCents += amount
		case "applied":
			summary.AppliedCount++
		}
	}
	return summary
}

func firstStringSubmatch(input, expr string) string {
	match := regexp.MustCompile(expr).FindStringSubmatch(input)
	if len(match) != 2 {
		return ""
	}
	return match[1]
}

func firstIntSubmatch(input, expr string) int {
	raw := firstStringSubmatch(input, expr)
	if raw == "" {
		return 0
	}
	n, _ := strconv.Atoi(raw)
	return n
}

func parseOpenCodeGoConsoleDate(raw string) *time.Time {
	raw = strings.TrimSpace(strings.Trim(raw, `"`))
	if raw == "" || raw == "null" {
		return nil
	}
	t, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return nil
	}
	utc := t.UTC()
	return &utc
}

func parseOpenCodeGoConsoleDateExpression(raw string) *time.Time {
	raw = strings.TrimSpace(raw)
	if raw == "null" || raw == "" {
		return nil
	}
	if match := regexp.MustCompile(`new Date\("([^"]+)"\)`).FindStringSubmatch(raw); len(match) == 2 {
		return parseOpenCodeGoConsoleDate(match[1])
	}
	return parseOpenCodeGoConsoleDate(raw)
}

func MaskOpenCodeGoReferralEmail(email string) string {
	email = strings.TrimSpace(email)
	if email == "" {
		return ""
	}
	local, domain, ok := strings.Cut(email, "@")
	if !ok || local == "" || domain == "" {
		return ""
	}
	if len(local) == 1 {
		return local + "*@" + domain
	}
	return local[:1] + strings.Repeat("*", len(local)-1) + "@" + domain
}

func ParseOpenCodeGoServerActions(assets map[string]string) (OpenCodeGoServerActions, error) {
	var actions OpenCodeGoServerActions
	for _, js := range assets {
		applyOpenCodeGoActionMappings(&actions, parseOpenCodeGoServerActionMappings(js))
	}
	if actions.ReferralUsagePreview == "" {
		return actions, fmt.Errorf("opencode go action hash not found: go.referral.usagePreview")
	}
	if actions.ReferralRewardApply == "" {
		return actions, fmt.Errorf("opencode go action hash not found: go.referral.reward.apply")
	}
	return actions, nil
}

func parseOpenCodeGoServerActionMappings(js string) map[string]string {
	refs := map[string]string{}
	for _, match := range opencodeGoServerRefConstPattern.FindAllStringSubmatch(js, -1) {
		if len(match) == 3 {
			refs[match[1]] = match[2]
		}
	}

	mappings := map[string]string{}
	for _, match := range opencodeGoServerRefDirectPattern.FindAllStringSubmatch(js, -1) {
		if len(match) == 3 {
			mappings[match[2]] = match[1]
		}
	}
	for _, match := range opencodeGoServerRefUsePattern.FindAllStringSubmatch(js, -1) {
		if len(match) != 3 {
			continue
		}
		if hash := refs[match[1]]; hash != "" {
			mappings[match[2]] = hash
		}
	}
	return mappings
}

func applyOpenCodeGoActionMappings(actions *OpenCodeGoServerActions, mappings map[string]string) {
	if actions == nil {
		return
	}
	if hash := mappings["go.referral.usagePreview"]; hash != "" {
		actions.ReferralUsagePreview = hash
	}
	if hash := mappings["go.referral.reward.apply"]; hash != "" {
		actions.ReferralRewardApply = hash
	}
	if hash := mappings["lite.subscription.get"]; hash != "" {
		actions.LiteSubscriptionGet = hash
	}
}
