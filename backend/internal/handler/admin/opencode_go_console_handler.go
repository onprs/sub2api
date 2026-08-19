package admin

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

var opencodeGoConsoleAuthTickets = service.NewOpenCodeGoConsoleAuthTicketStore(10 * time.Minute)

type openCodeGoConsoleAuthTicketRequest struct {
	Workspace string `json:"workspace" binding:"required"`
}

type openCodeGoConsoleAuthCompleteRequest struct {
	TicketID        string `json:"ticket_id"`
	WorkspaceID     string `json:"workspace_id" binding:"required"`
	AuthCookie      string `json:"auth_cookie" binding:"required"`
	CookieExpiresAt string `json:"cookie_expires_at"`
}

type openCodeGoConsoleAuthTicketResponse struct {
	TicketID      string    `json:"ticket_id"`
	ExpiresAt     time.Time `json:"expires_at"`
	WorkspaceID   string    `json:"workspace_id"`
	HelperURL     string    `json:"helper_url"`
	HelperCommand string    `json:"helper_command"`
}

type openCodeGoConsoleCompleteResponse struct {
	AccountID   int64                             `json:"account_id"`
	WorkspaceID string                            `json:"workspace_id"`
	Summary     *openCodeGoConsoleSummaryResponse `json:"summary"`
}

type openCodeGoConsoleSummaryResponse struct {
	Authorized     bool                               `json:"authorized"`
	AuthStatus     string                             `json:"auth_status"`
	AuthCheckedAt  string                             `json:"auth_checked_at,omitempty"`
	WorkspaceID    string                             `json:"workspace_id,omitempty"`
	UsageSource    string                             `json:"usage_source,omitempty"`
	UsageUpdatedAt string                             `json:"usage_updated_at,omitempty"`
	Usage          openCodeGoConsoleUsageResponse     `json:"usage"`
	Referral       openCodeGoReferralSummaryResponse  `json:"referral"`
	Rewards        []service.OpenCodeGoReferralReward `json:"rewards,omitempty"`
	Error          string                             `json:"error,omitempty"`
}

type openCodeGoConsoleUsageResponse struct {
	FiveHour  openCodeGoConsoleUsageWindowResponse `json:"five_hour"`
	SevenDay  openCodeGoConsoleUsageWindowResponse `json:"seven_day"`
	ThirtyDay openCodeGoConsoleUsageWindowResponse `json:"thirty_day"`
}

type openCodeGoConsoleUsageWindowResponse struct {
	UsagePercent     float64 `json:"usage_percent"`
	ResetInSec       int     `json:"reset_in_sec"`
	ResetsAt         string  `json:"resets_at,omitempty"`
	RemainingSeconds int     `json:"remaining_seconds"`
}

type openCodeGoReferralSummaryResponse struct {
	ReferralCode         string `json:"referral_code,omitempty"`
	InviteLink           string `json:"invite_link,omitempty"`
	RewardAmountCents    int    `json:"reward_amount_cents,omitempty"`
	AvailableCount       int    `json:"available_count"`
	AvailableAmountCents int    `json:"available_amount_cents"`
	AppliedCount         int    `json:"applied_count"`
	UpdatedAt            string `json:"updated_at,omitempty"`
}

type openCodeGoReferralPreviewResponse struct {
	RewardID string                                  `json:"reward_id"`
	Preview  *service.OpenCodeGoReferralUsagePreview `json:"preview"`
}

type openCodeGoReferralApplyResponse struct {
	AppliedAmountCents int                                `json:"applied_amount_cents"`
	Usage              openCodeGoConsoleUsageResponse     `json:"usage"`
	Referral           openCodeGoReferralSummaryResponse  `json:"referral"`
	Rewards            []service.OpenCodeGoReferralReward `json:"rewards"`
}

func (h *AccountHandler) CreateOpenCodeGoConsoleAuthTicket(c *gin.Context) {
	accountID, ok := parseOpenCodeGoConsoleAccountID(c)
	if !ok {
		return
	}
	var req openCodeGoConsoleAuthTicketRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request body")
		return
	}
	workspaceID := normalizeOpenCodeGoWorkspaceID(req.Workspace)
	if workspaceID == "" {
		response.BadRequest(c, "Invalid OpenCode workspace")
		return
	}
	account, ok := h.getOpenCodeGoAPIKeyAccount(c, accountID)
	if !ok {
		return
	}
	if !account.IsOpenCodeGoAPIKey() {
		response.BadRequest(c, "Only OpenCode Go API key accounts support Console auth")
		return
	}

	ticket, err := opencodeGoConsoleAuthTickets.Create(accountID, workspaceID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	helperURL := absoluteRequestURL(c, "/api/v1/opencode-go/console-auth/helper.ps1?ticket="+url.QueryEscape(ticket.ID))
	response.Success(c, openCodeGoConsoleAuthTicketResponse{
		TicketID:      ticket.ID,
		ExpiresAt:     ticket.ExpiresAt,
		WorkspaceID:   workspaceID,
		HelperURL:     helperURL,
		HelperCommand: fmt.Sprintf("powershell -NoProfile -ExecutionPolicy Bypass -Command \"iwr '%s' -UseBasicParsing | iex\"", helperURL),
	})
}

func (h *AccountHandler) OpenCodeGoConsoleAuthHelper(c *gin.Context) {
	ticketID := strings.TrimSpace(c.Query("ticket"))
	ticket, err := opencodeGoConsoleAuthTickets.Peek(ticketID)
	if err != nil {
		writeOpenCodeGoTicketError(c, err)
		return
	}
	completeURL := absoluteRequestURL(c, "/api/v1/opencode-go/console-auth/complete")
	c.Header("Content-Type", "text/plain; charset=utf-8")
	c.String(http.StatusOK, buildOpenCodeGoConsoleAuthHelperScript(ticket.ID, ticket.WorkspaceID, completeURL))
}

func (h *AccountHandler) CompleteOpenCodeGoConsoleAuth(c *gin.Context) {
	var req openCodeGoConsoleAuthCompleteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request body")
		return
	}
	if req.TicketID == "" {
		req.TicketID = strings.TrimSpace(c.Query("ticket"))
	}
	workspaceID := normalizeOpenCodeGoWorkspaceID(req.WorkspaceID)
	if workspaceID == "" {
		response.BadRequest(c, "Invalid OpenCode workspace")
		return
	}
	ticket, err := opencodeGoConsoleAuthTickets.Peek(req.TicketID)
	if err != nil {
		writeOpenCodeGoTicketError(c, err)
		return
	}

	cookie := normalizeOpenCodeGoConsoleCookie(req.AuthCookie)
	client := service.NewOpenCodeGoConsoleClient("", nil)
	summary, err := client.FetchSummary(c.Request.Context(), workspaceID, cookie)
	if err != nil {
		response.ErrorFrom(c, mapOpenCodeGoConsoleFetchError(ticket.AccountID, h, c.Request.Context(), err))
		return
	}
	consumed, err := opencodeGoConsoleAuthTickets.ConsumeWithObservedWorkspace(req.TicketID, workspaceID)
	if err != nil {
		writeOpenCodeGoTicketError(c, err)
		return
	}
	account, ok := h.getOpenCodeGoAPIKeyAccount(c, consumed.AccountID)
	if !ok {
		return
	}
	if err := h.saveOpenCodeGoConsoleCredentials(c.Request.Context(), account, cookie, workspaceID, "windows_cdp_helper", req.CookieExpiresAt); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	if err := h.accountUsageService.PersistOpenCodeGoConsoleSummary(c.Request.Context(), account.ID, summary, service.OpenCodeGoConsoleAuthStatusReady); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, openCodeGoConsoleCompleteResponse{
		AccountID:   account.ID,
		WorkspaceID: workspaceID,
		Summary:     buildOpenCodeGoConsoleSummaryResponse(account, summary, ""),
	})
}

func (h *AccountHandler) TestOpenCodeGoConsoleAuth(c *gin.Context) {
	accountID, ok := parseOpenCodeGoConsoleAccountID(c)
	if !ok {
		return
	}
	account, ok := h.getOpenCodeGoAPIKeyAccount(c, accountID)
	if !ok {
		return
	}
	summary, err := h.fetchAndPersistOpenCodeGoConsoleSummary(c.Request.Context(), account)
	if err != nil {
		response.ErrorFrom(c, mapOpenCodeGoConsoleFetchError(account.ID, h, c.Request.Context(), err))
		return
	}
	response.Success(c, buildOpenCodeGoConsoleSummaryResponse(account, summary, ""))
}

func (h *AccountHandler) ClearOpenCodeGoConsoleAuth(c *gin.Context) {
	accountID, ok := parseOpenCodeGoConsoleAccountID(c)
	if !ok {
		return
	}
	account, ok := h.getOpenCodeGoAPIKeyAccount(c, accountID)
	if !ok {
		return
	}
	creds := cloneAccountCredentials(account.Credentials)
	for _, key := range []string{"console_cookie", "console_workspace_id", "console_auth_source", "console_auth_imported_at", "console_auth_expires_at"} {
		creds[key] = ""
	}
	if _, err := h.adminService.UpdateAccount(c.Request.Context(), account.ID, &service.UpdateAccountInput{Credentials: creds, SkipMixedChannelCheck: true}); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	_ = h.adminService.UpdateAccountExtra(c.Request.Context(), account.ID, map[string]any{
		"opencode_go_console_auth_status":     "",
		"opencode_go_console_auth_checked_at": time.Now().UTC().Format(time.RFC3339),
		"opencode_go_usage_source":            "estimated",
		"console_workspace_id":                "",
		"console_auth_source":                 "",
		"console_auth_imported_at":            "",
		"console_auth_expires_at":             "",
	})
	response.Success(c, map[string]any{"cleared": true})
}

func (h *AccountHandler) GetOpenCodeGoConsoleSummary(c *gin.Context) {
	accountID, ok := parseOpenCodeGoConsoleAccountID(c)
	if !ok {
		return
	}
	account, ok := h.getOpenCodeGoAPIKeyAccount(c, accountID)
	if !ok {
		return
	}
	summary, err := h.fetchAndPersistOpenCodeGoConsoleSummary(c.Request.Context(), account)
	if err != nil {
		response.Success(c, buildOpenCodeGoConsoleSummaryResponse(account, nil, err.Error()))
		return
	}
	response.Success(c, buildOpenCodeGoConsoleSummaryResponse(account, summary, ""))
}

func (h *AccountHandler) PreviewOpenCodeGoReferralReward(c *gin.Context) {
	account, rewardID, ok := h.openCodeGoReferralAccountAndReward(c)
	if !ok {
		return
	}
	if _, err := h.requireOpenCodeGoRewardStatus(c.Request.Context(), account, rewardID, "available"); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	preview, err := service.NewOpenCodeGoReferralActionClient("", nil).Preview(
		c.Request.Context(),
		openCodeGoConsoleWorkspaceID(account),
		rewardID,
		openCodeGoConsoleCookie(account),
	)
	if err != nil {
		response.ErrorFrom(c, mapOpenCodeGoConsoleFetchError(account.ID, h, c.Request.Context(), err))
		return
	}
	response.Success(c, openCodeGoReferralPreviewResponse{RewardID: rewardID, Preview: preview})
}

func (h *AccountHandler) ApplyOpenCodeGoReferralReward(c *gin.Context) {
	account, rewardID, ok := h.openCodeGoReferralAccountAndReward(c)
	if !ok {
		return
	}
	reward, err := h.requireOpenCodeGoRewardStatus(c.Request.Context(), account, rewardID, "available")
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	actionClient := service.NewOpenCodeGoReferralActionClient("", nil)
	err = actionClient.Apply(c.Request.Context(), openCodeGoConsoleWorkspaceID(account), rewardID, openCodeGoConsoleCookie(account))
	if err != nil {
		if errors.Is(err, service.ErrOpenCodeGoReferralRewardAlreadyApplied) {
			_, _ = h.fetchAndPersistOpenCodeGoConsoleSummary(c.Request.Context(), account)
		}
		response.ErrorFrom(c, mapOpenCodeGoReferralActionError(account.ID, h, c.Request.Context(), err))
		return
	}
	summary, err := h.fetchAndPersistOpenCodeGoConsoleSummary(c.Request.Context(), account)
	if err != nil {
		response.ErrorFrom(c, mapOpenCodeGoConsoleFetchError(account.ID, h, c.Request.Context(), err))
		return
	}
	resp := openCodeGoReferralApplyResponse{
		AppliedAmountCents: reward.AmountCents,
		Usage:              buildOpenCodeGoConsoleUsageResponse(summary),
		Referral:           buildOpenCodeGoReferralSummaryResponse(summary),
		Rewards:            summary.Referral.Rewards,
	}
	response.Success(c, resp)
}

func (h *AccountHandler) fetchAndPersistOpenCodeGoConsoleSummary(ctx context.Context, account *service.Account) (*service.OpenCodeGoConsoleSummary, error) {
	cookie := openCodeGoConsoleCookie(account)
	workspaceID := openCodeGoConsoleWorkspaceID(account)
	if cookie == "" || workspaceID == "" {
		return nil, service.ErrOpenCodeGoConsoleAuthExpired
	}
	summary, err := service.NewOpenCodeGoConsoleClient("", nil).FetchSummary(ctx, workspaceID, cookie)
	if err != nil {
		return nil, err
	}
	if h.accountUsageService != nil {
		summary, _ = h.accountUsageService.AutoApplyOpenCodeGoReferralRewardsIfEligible(ctx, account.ID, workspaceID, cookie, summary)
	}
	if err := h.accountUsageService.PersistOpenCodeGoConsoleSummary(ctx, account.ID, summary, service.OpenCodeGoConsoleAuthStatusReady); err != nil {
		return nil, err
	}
	return summary, nil
}

func (h *AccountHandler) requireOpenCodeGoRewardStatus(ctx context.Context, account *service.Account, rewardID, status string) (*service.OpenCodeGoReferralReward, error) {
	summary, err := h.fetchAndPersistOpenCodeGoConsoleSummary(ctx, account)
	if err != nil {
		return nil, err
	}
	for i := range summary.Referral.Rewards {
		reward := &summary.Referral.Rewards[i]
		if reward.ID == rewardID {
			if reward.Status != status {
				return nil, infraerrors.Conflict("reward_not_available", "reward is not available")
			}
			return reward, nil
		}
	}
	return nil, infraerrors.NotFound("reward_not_found", "reward not found")
}

func (h *AccountHandler) saveOpenCodeGoConsoleCredentials(ctx context.Context, account *service.Account, cookie, workspaceID, source, expiresAt string) error {
	creds := cloneAccountCredentials(account.Credentials)
	now := time.Now().UTC().Format(time.RFC3339)
	creds["console_cookie"] = cookie
	creds["console_workspace_id"] = workspaceID
	creds["console_auth_source"] = source
	creds["console_auth_imported_at"] = now
	creds["console_auth_expires_at"] = strings.TrimSpace(expiresAt)
	if _, err := h.adminService.UpdateAccount(ctx, account.ID, &service.UpdateAccountInput{Credentials: creds, SkipMixedChannelCheck: true}); err != nil {
		return err
	}
	return h.adminService.UpdateAccountExtra(ctx, account.ID, map[string]any{
		"console_workspace_id":     workspaceID,
		"console_auth_source":      source,
		"console_auth_imported_at": now,
		"console_auth_expires_at":  strings.TrimSpace(expiresAt),
	})
}

func parseOpenCodeGoConsoleAccountID(c *gin.Context) (int64, bool) {
	accountID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || accountID <= 0 {
		response.BadRequest(c, "Invalid account ID")
		return 0, false
	}
	return accountID, true
}

func (h *AccountHandler) getOpenCodeGoAPIKeyAccount(c *gin.Context, accountID int64) (*service.Account, bool) {
	account, err := h.adminService.GetAccount(c.Request.Context(), accountID)
	if err != nil {
		response.ErrorFrom(c, err)
		return nil, false
	}
	if account == nil || !account.IsOpenCodeGoAPIKey() {
		response.BadRequest(c, "Only OpenCode Go API key accounts support this operation")
		return nil, false
	}
	return account, true
}

func (h *AccountHandler) openCodeGoReferralAccountAndReward(c *gin.Context) (*service.Account, string, bool) {
	accountID, ok := parseOpenCodeGoConsoleAccountID(c)
	if !ok {
		return nil, "", false
	}
	account, ok := h.getOpenCodeGoAPIKeyAccount(c, accountID)
	if !ok {
		return nil, "", false
	}
	rewardID := strings.TrimSpace(c.Param("reward_id"))
	if rewardID == "" {
		response.BadRequest(c, "Invalid reward ID")
		return nil, "", false
	}
	return account, rewardID, true
}

func normalizeOpenCodeGoWorkspaceID(input string) string {
	input = strings.TrimSpace(input)
	if input == "" {
		return ""
	}
	if parsed, err := url.Parse(input); err == nil && parsed.Host != "" {
		parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
		for i := 0; i+1 < len(parts); i++ {
			if parts[i] == "workspace" && strings.HasPrefix(parts[i+1], "wrk_") {
				return parts[i+1]
			}
		}
	}
	if strings.HasPrefix(input, "wrk_") {
		return input
	}
	return ""
}

func normalizeOpenCodeGoConsoleCookie(cookie string) string {
	cookie = strings.TrimSpace(cookie)
	if cookie == "" {
		return ""
	}
	if strings.Contains(cookie, "=") {
		return cookie
	}
	return "auth=" + cookie
}

func openCodeGoConsoleCookie(account *service.Account) string {
	if account == nil || account.Credentials == nil {
		return ""
	}
	return openCodeGoConsoleStringValue(account.Credentials, "console_cookie")
}

func openCodeGoConsoleWorkspaceID(account *service.Account) string {
	if account == nil {
		return ""
	}
	if workspaceID := openCodeGoConsoleStringValue(account.Credentials, "console_workspace_id"); workspaceID != "" {
		return workspaceID
	}
	if workspaceID := openCodeGoConsoleStringValue(account.Extra, "console_workspace_id"); workspaceID != "" {
		return workspaceID
	}
	return openCodeGoConsoleStringValue(account.Extra, "opencode_go_console_workspace_id")
}

func cloneAccountCredentials(in map[string]any) map[string]any {
	out := make(map[string]any, len(in)+4)
	for k, v := range in {
		out[k] = v
	}
	return out
}

func absoluteRequestURL(c *gin.Context, path string) string {
	scheme := strings.TrimSpace(c.GetHeader("X-Forwarded-Proto"))
	if scheme == "" {
		scheme = "http"
		if c.Request != nil && c.Request.TLS != nil {
			scheme = "https"
		}
	}
	host := ""
	if c.Request != nil {
		host = c.Request.Host
	}
	if forwardedHost := strings.TrimSpace(c.GetHeader("X-Forwarded-Host")); forwardedHost != "" {
		host = forwardedHost
	}
	return scheme + "://" + host + path
}

func buildOpenCodeGoConsoleSummaryResponse(account *service.Account, summary *service.OpenCodeGoConsoleSummary, errMsg string) *openCodeGoConsoleSummaryResponse {
	resp := &openCodeGoConsoleSummaryResponse{
		Authorized:  openCodeGoConsoleCookie(account) != "" && openCodeGoConsoleWorkspaceID(account) != "",
		AuthStatus:  service.OpenCodeGoConsoleAuthStatusReady,
		WorkspaceID: openCodeGoConsoleWorkspaceID(account),
		Error:       errMsg,
	}
	if account != nil && account.Extra != nil {
		if v := openCodeGoConsoleStringValue(account.Extra, "opencode_go_console_auth_status"); v != "" {
			resp.AuthStatus = v
		}
		resp.AuthCheckedAt = openCodeGoConsoleStringValue(account.Extra, "opencode_go_console_auth_checked_at")
		resp.UsageSource = openCodeGoConsoleStringValue(account.Extra, "opencode_go_usage_source")
		resp.UsageUpdatedAt = openCodeGoConsoleStringValue(account.Extra, "opencode_go_usage_updated_at")
		resp.Referral.UpdatedAt = openCodeGoConsoleStringValue(account.Extra, "opencode_go_referral_updated_at")
	}
	if !resp.Authorized && resp.AuthStatus == service.OpenCodeGoConsoleAuthStatusReady {
		resp.AuthStatus = ""
	}
	if summary != nil {
		resp.WorkspaceID = summary.WorkspaceID
		resp.AuthStatus = service.OpenCodeGoConsoleAuthStatusReady
		resp.UsageSource = "official_console"
		resp.UsageUpdatedAt = summary.FetchedAt.UTC().Format(time.RFC3339)
		resp.Usage = buildOpenCodeGoConsoleUsageResponse(summary)
		resp.Referral = buildOpenCodeGoReferralSummaryResponse(summary)
		resp.Rewards = summary.Referral.Rewards
	}
	return resp
}

func openCodeGoConsoleStringValue(values map[string]any, key string) string {
	if values == nil {
		return ""
	}
	value, ok := values[key]
	if !ok || value == nil {
		return ""
	}
	text := strings.TrimSpace(fmt.Sprint(value))
	switch strings.ToLower(text) {
	case "", "<nil>", "null", "undefined":
		return ""
	default:
		return text
	}
}

func buildOpenCodeGoConsoleUsageResponse(summary *service.OpenCodeGoConsoleSummary) openCodeGoConsoleUsageResponse {
	if summary == nil {
		return openCodeGoConsoleUsageResponse{}
	}
	return openCodeGoConsoleUsageResponse{
		FiveHour:  buildOpenCodeGoConsoleUsageWindowResponse(summary.Usage.FiveHour),
		SevenDay:  buildOpenCodeGoConsoleUsageWindowResponse(summary.Usage.SevenDay),
		ThirtyDay: buildOpenCodeGoConsoleUsageWindowResponse(summary.Usage.ThirtyDay),
	}
}

func buildOpenCodeGoConsoleUsageWindowResponse(window service.OpenCodeGoUsageWindow) openCodeGoConsoleUsageWindowResponse {
	resp := openCodeGoConsoleUsageWindowResponse{
		UsagePercent: window.UsagePercent,
		ResetInSec:   window.ResetInSec,
	}
	if window.ResetsAt != nil {
		resp.ResetsAt = window.ResetsAt.UTC().Format(time.RFC3339)
		remaining := int(time.Until(*window.ResetsAt).Seconds())
		if remaining > 0 {
			resp.RemainingSeconds = remaining
		}
	}
	return resp
}

func buildOpenCodeGoReferralSummaryResponse(summary *service.OpenCodeGoConsoleSummary) openCodeGoReferralSummaryResponse {
	if summary == nil {
		return openCodeGoReferralSummaryResponse{}
	}
	referral := summary.Referral
	resp := openCodeGoReferralSummaryResponse{
		ReferralCode:         referral.ReferralCode,
		RewardAmountCents:    referral.RewardAmountCents,
		AvailableCount:       referral.AvailableCount,
		AvailableAmountCents: referral.AvailableAmountCents,
		AppliedCount:         referral.AppliedCount,
		UpdatedAt:            summary.FetchedAt.UTC().Format(time.RFC3339),
	}
	if referral.ReferralCode != "" {
		resp.InviteLink = "https://opencode.ai/go?ref=" + url.QueryEscape(referral.ReferralCode)
	}
	return resp
}

func mapOpenCodeGoConsoleFetchError(accountID int64, h *AccountHandler, ctx context.Context, err error) error {
	if errors.Is(err, service.ErrOpenCodeGoConsoleAuthExpired) {
		_ = h.adminService.UpdateAccountExtra(ctx, accountID, map[string]any{
			"opencode_go_console_auth_status":     service.OpenCodeGoConsoleAuthStatusExpired,
			"opencode_go_console_auth_checked_at": time.Now().UTC().Format(time.RFC3339),
		})
		return infraerrors.Unauthorized("opencode_go_console_auth_expired", "OpenCode Go Console login expired")
	}
	_ = h.adminService.UpdateAccountExtra(ctx, accountID, map[string]any{
		"opencode_go_console_auth_status":     service.OpenCodeGoConsoleAuthStatusError,
		"opencode_go_console_auth_checked_at": time.Now().UTC().Format(time.RFC3339),
	})
	return infraerrors.BadRequest("opencode_go_console_fetch_failed", err.Error())
}

func mapOpenCodeGoReferralActionError(accountID int64, h *AccountHandler, ctx context.Context, err error) error {
	if errors.Is(err, service.ErrOpenCodeGoReferralRewardAlreadyApplied) {
		return infraerrors.Conflict("reward_already_applied", "reward already applied")
	}
	return mapOpenCodeGoConsoleFetchError(accountID, h, ctx, err)
}

func writeOpenCodeGoTicketError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrOpenCodeGoConsoleAuthTicketExpired):
		response.Error(c, http.StatusBadRequest, "Console auth ticket expired")
	case errors.Is(err, service.ErrOpenCodeGoConsoleAuthTicketUsed):
		response.Error(c, http.StatusBadRequest, "Console auth ticket already used")
	case errors.Is(err, service.ErrOpenCodeGoConsoleAuthTicketMismatch):
		response.Error(c, http.StatusBadRequest, "Console auth ticket mismatch")
	default:
		response.NotFound(c, "Console auth ticket not found")
	}
}

func buildOpenCodeGoConsoleAuthHelperScript(ticketID, workspaceID, completeURL string) string {
	goURL := "https://opencode.ai/workspace/" + workspaceID + "/go"
	return fmt.Sprintf(`$ErrorActionPreference = 'Stop'
$Ticket = '%s'
$WorkspaceID = '%s'
$CompleteUrl = '%s'
$GoUrl = '%s'
$Port = Get-Random -Minimum 32000 -Maximum 39000
$Profile = Join-Path $env:TEMP ("sub2api-opencode-go-" + [guid]::NewGuid().ToString("N"))
New-Item -ItemType Directory -Force -Path $Profile | Out-Null
$Candidates = @(@(
  "$env:ProgramFiles\Google\Chrome\Application\chrome.exe",
  "${env:ProgramFiles(x86)}\Google\Chrome\Application\chrome.exe",
  "$env:LOCALAPPDATA\Google\Chrome\Application\chrome.exe",
  "$env:ProgramFiles\Microsoft\Edge\Application\msedge.exe",
  "${env:ProgramFiles(x86)}\Microsoft\Edge\Application\msedge.exe",
  (Get-Command chrome.exe -ErrorAction SilentlyContinue | Select-Object -First 1 -ExpandProperty Source),
  (Get-Command msedge.exe -ErrorAction SilentlyContinue | Select-Object -First 1 -ExpandProperty Source)
) | Where-Object { $_ -and (Test-Path $_) } | Select-Object -Unique)
if (-not $Candidates) { throw "Chrome or Edge was not found. Install Chrome/Edge or add it to PATH." }
$Browser = $Candidates[0]
$Args = @("--user-data-dir=$Profile", "--remote-debugging-port=$Port", "--no-first-run", "--new-window", $GoUrl)
$Proc = Start-Process -FilePath $Browser -ArgumentList $Args -PassThru
Write-Host "Opened a temporary browser profile. Log in to opencode.ai, then keep this window open."
Start-Sleep -Seconds 3
try { [System.Net.WebSockets.ClientWebSocket] | Out-Null } catch { throw "ClientWebSocket is unavailable. Please run Windows PowerShell 5.1+ or PowerShell 7+." }
function Invoke-Json($Url) {
  for ($i = 0; $i -lt 120; $i++) {
    try { return Invoke-RestMethod -Uri $Url -UseBasicParsing -TimeoutSec 2 } catch { Start-Sleep -Seconds 1 }
  }
  throw "Timed out waiting for Chrome DevTools."
}
$Tabs = Invoke-Json "http://127.0.0.1:$Port/json"
$Tab = @($Tabs | Where-Object { $_.url -like "*opencode.ai*" } | Select-Object -First 1)[0]
if (-not $Tab) { $Tab = @($Tabs | Select-Object -First 1)[0] }
$WsUrl = $Tab.webSocketDebuggerUrl
$Client = [System.Net.WebSockets.ClientWebSocket]::new()
$Client.ConnectAsync([Uri]$WsUrl, [Threading.CancellationToken]::None).GetAwaiter().GetResult() | Out-Null
$Seq = 0
function Send-CDP($Method, $Params) {
  $script:Seq++
  $Payload = @{ id = $script:Seq; method = $Method; params = $Params } | ConvertTo-Json -Depth 10 -Compress
  $Bytes = [Text.Encoding]::UTF8.GetBytes($Payload)
  $Seg = [ArraySegment[byte]]::new($Bytes)
  $Client.SendAsync($Seg, [System.Net.WebSockets.WebSocketMessageType]::Text, $true, [Threading.CancellationToken]::None).GetAwaiter().GetResult()
  while ($true) {
    $Buffer = New-Object byte[] 262144
    $Builder = [System.Text.StringBuilder]::new()
    do {
      $Out = [ArraySegment[byte]]::new($Buffer)
      $Res = $Client.ReceiveAsync($Out, [Threading.CancellationToken]::None).GetAwaiter().GetResult()
      if ($Res.MessageType -eq [System.Net.WebSockets.WebSocketMessageType]::Close) { throw "Chrome DevTools WebSocket closed." }
      [void]$Builder.Append([Text.Encoding]::UTF8.GetString($Buffer, 0, $Res.Count))
    } while (-not $Res.EndOfMessage)
    $Text = $Builder.ToString()
    if ($Text -match '"id":\s*' + $script:Seq) { return ($Text | ConvertFrom-Json) }
  }
}
Send-CDP "Network.enable" @{} | Out-Null
Send-CDP "Page.enable" @{} | Out-Null
Send-CDP "Runtime.enable" @{} | Out-Null
function Build-OpenCodeCookieHeader($Cookies) {
  $Pairs = @($Cookies |
    Where-Object { $_.name -and ($null -ne $_.value) -and $_.domain -like "*opencode.ai*" } |
    ForEach-Object { "$($_.name)=$($_.value)" })
  return ($Pairs -join "; ")
}
function Get-OpenCodeCookieNames($Cookies) {
  $Names = @($Cookies |
    Where-Object { $_.name -and $_.domain -like "*opencode.ai*" } |
    ForEach-Object { $_.name } |
    Sort-Object -Unique)
  return ($Names -join ",")
}
function Read-OpenCodeGoBrowserState {
  $Expression = @'
(() => {
  const href = location.href || "";
  const html = document.documentElement ? document.documentElement.outerHTML : "";
  const text = document.body ? document.body.innerText : "";
  const workspaceMatch = href.match(/\/workspace\/(wrk_[A-Za-z0-9]+)(?:\/go)?(?:[/?#]|$)/);
  return JSON.stringify({
    href,
    workspaceID: workspaceMatch ? workspaceMatch[1] : "",
    readyState: document.readyState || "",
    markerLiteSubscription: html.includes("lite.subscription.get") || html.includes("rollingUsage"),
    markerReferral: html.includes("go.referral.get") || html.includes("go-referral-section"),
    markerGoPage: html.includes('data-page="workspace"') || text.includes("OpenCode Go") || text.includes("低成本编码模型") || text.includes("滚动用量") || text.includes("Rolling Usage")
  });
})()
'@
  try {
    $Eval = Send-CDP "Runtime.evaluate" @{ expression = $Expression; returnByValue = $true; awaitPromise = $true }
    $Raw = $Eval.result.result.value
    if (-not $Raw) { $Raw = $Eval.result.result.description }
    if (-not $Raw) { return [pscustomobject]@{ href = ""; readyState = ""; markerLiteSubscription = $false; markerReferral = $false; markerGoPage = $false } }
    return ($Raw | ConvertFrom-Json)
  } catch {
    return [pscustomobject]@{ href = ""; readyState = "error"; markerLiteSubscription = $false; markerReferral = $false; markerGoPage = $false }
  }
}
function Test-OpenCodeGoBrowserPage($State) {
  if (-not $State) { return $false }
  $Href = [string]$State.href
  $Ready = ([string]$State.readyState) -eq "interactive" -or ([string]$State.readyState) -eq "complete"
  $OnWorkspacePage = $Href -match "/workspace/wrk_[A-Za-z0-9]+(?:/go)?"
  $HasWorkspaceID = ([string]$State.workspaceID) -ne ""
  return ($Ready -and $OnWorkspacePage -and $HasWorkspaceID)
}
function Format-OpenCodeBool($Value) {
  if ($Value) { return "true" }
  return "false"
}
function Format-OpenCodeSafeUrl($Url) {
  if (-not $Url) { return "" }
  try {
    $Uri = [Uri]$Url
    return ($Uri.Scheme + "://" + $Uri.Host + $Uri.AbsolutePath)
  } catch {
    return ""
  }
}
function Write-OpenCodeWaitDiagnostics($Cookies, $Auth, $State, $GoPageLoaded) {
  $CookieCount = @($Cookies | Where-Object { $_.name -and $_.domain -like "*opencode.ai*" }).Count
  $CookieNames = Get-OpenCodeCookieNames $Cookies
  if ($CookieNames.Length -gt 160) { $CookieNames = $CookieNames.Substring(0, 160) + "..." }
  $Href = ""
  $Ready = ""
  $CurrentWorkspace = ""
  $MarkerLite = $false
  $MarkerReferral = $false
  if ($State) {
    $Href = Format-OpenCodeSafeUrl $State.href
    $Ready = [string]$State.readyState
    $CurrentWorkspace = [string]$State.workspaceID
    $MarkerLite = [bool]$State.markerLiteSubscription
    $MarkerReferral = [bool]$State.markerReferral
  }
  Write-Host ("Waiting for opencode.ai login cookie and valid Go page session... cookie_count={0} cookie_names={1} has_auth={2} browser_url={3} ready_state={4} go_page_loaded={5} marker_lite_subscription={6} marker_referral={7} current_workspace={8} target_workspace={9}" -f $CookieCount, $CookieNames, (Format-OpenCodeBool $Auth), $Href, $Ready, (Format-OpenCodeBool $GoPageLoaded), (Format-OpenCodeBool $MarkerLite), (Format-OpenCodeBool $MarkerReferral), $CurrentWorkspace, $WorkspaceID)
}
Write-Host "Waiting for opencode.ai login cookie. Finish login in the opened browser window."
$Auth = $null
$CookieHeader = ""
$State = $null
$GoPageLoaded = $false
$ResolvedWorkspaceID = ""
for ($i = 0; $i -lt 300; $i++) {
  $Cookies = @((Send-CDP "Network.getCookies" @{ urls = @($GoUrl) }).result.cookies)
  $Auth = @($Cookies | Where-Object { $_.name -eq "auth" -and $_.domain -like "*opencode.ai*" } | Select-Object -First 1)[0]
  $CookieHeader = Build-OpenCodeCookieHeader $Cookies
  $State = Read-OpenCodeGoBrowserState
  $GoPageLoaded = Test-OpenCodeGoBrowserPage $State
  $ResolvedWorkspaceID = [string]$State.workspaceID
  if (-not $ResolvedWorkspaceID) { $ResolvedWorkspaceID = $WorkspaceID }
  if ($Auth -and $CookieHeader -and $GoPageLoaded) { break }
  if (($i %% 15) -eq 0) { Write-OpenCodeWaitDiagnostics $Cookies $Auth $State $GoPageLoaded }
  $Auth = $null
  Start-Sleep -Seconds 2
}
if (-not $Auth -or -not $CookieHeader -or -not $GoPageLoaded -or -not $ResolvedWorkspaceID) { throw "Timed out waiting for a valid opencode.ai workspace session. Please run a new command, finish login in the opened browser, and make sure the browser is on an opencode.ai workspace page." }
$ExpiresAt = ""
if ($Auth.expires -and $Auth.expires -gt 0) {
  $ExpiresAt = ([DateTimeOffset]::FromUnixTimeSeconds([int64]$Auth.expires)).UtcDateTime.ToString("o")
}
$Body = @{
  ticket_id = $Ticket
  workspace_id = $ResolvedWorkspaceID
  auth_cookie = $CookieHeader
  cookie_expires_at = $ExpiresAt
} | ConvertTo-Json -Depth 5
$Resp = Invoke-RestMethod -Method Post -Uri $CompleteUrl -ContentType "application/json" -Body $Body -UseBasicParsing
Write-Host "OpenCode Go Console auth imported into Sub2API."
try { $Client.Dispose() } catch {}
`, psEscape(ticketID), psEscape(workspaceID), psEscape(completeURL), psEscape(goURL))
}

func psEscape(s string) string {
	return strings.ReplaceAll(s, "'", "''")
}
