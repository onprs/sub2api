package service

import (
	"context"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
)

const (
	UserRequestRecordSuccess = "success"
	UserRequestRecordError   = "error"
)

func IsUserRequestCategory(value string) bool {
	switch value {
	case "", "success", "auth", "rate_limit", "quota", "invalid_request", "service_unavailable", "upstream", "internal", "cyber", "other":
		return true
	default:
		return false
	}
}

type UserRequestHistoryFilter struct {
	UserID        int64
	APIKeyID      int64
	GroupID       int64
	Model         string
	RequestType   *int16
	Stream        *bool
	BillingType   *int8
	BillingMode   string
	Category      string
	StatusCode    *int
	StartTime     *time.Time
	EndTime       *time.Time
	IncludeErrors bool
}

type UserRequestHistoryKey struct {
	ID      int64  `json:"id"`
	Name    string `json:"name"`
	Deleted bool   `json:"deleted"`
}

type UserRequestHistoryGroup struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// UserRequestRecord is the redacted common read model for successful and failed
// requests owned by one user. Error-only internal fields never enter this type.
type UserRequestRecord struct {
	RecordType string    `json:"record_type"`
	ID         int64     `json:"id"`
	UserID     int64     `json:"user_id"`
	CreatedAt  time.Time `json:"created_at"`
	RequestID  string    `json:"request_id"`
	StatusCode int       `json:"status_code"`
	Category   string    `json:"category"`
	Model      string    `json:"model"`
	Platform   string    `json:"platform,omitempty"`
	Message    string    `json:"message,omitempty"`

	APIKeyID       *int64                   `json:"api_key_id"`
	APIKey         *UserRequestHistoryKey   `json:"api_key,omitempty"`
	AccountID      *int64                   `json:"account_id"`
	GroupID        *int64                   `json:"group_id"`
	Group          *UserRequestHistoryGroup `json:"group,omitempty"`
	SubscriptionID *int64                   `json:"subscription_id"`

	ServiceTier     *string `json:"service_tier,omitempty"`
	ReasoningEffort *string `json:"reasoning_effort,omitempty"`
	InboundEndpoint *string `json:"inbound_endpoint,omitempty"`
	RequestType     string  `json:"request_type"`
	Stream          bool    `json:"stream"`
	OpenAIWSMode    bool    `json:"openai_ws_mode"`
	UserAgent       *string `json:"user_agent,omitempty"`
	IPAddress       *string `json:"ip_address,omitempty"`

	InputTokens           int  `json:"input_tokens"`
	OutputTokens          int  `json:"output_tokens"`
	CacheCreationTokens   int  `json:"cache_creation_tokens"`
	CacheReadTokens       int  `json:"cache_read_tokens"`
	CacheCreation5mTokens int  `json:"cache_creation_5m_tokens"`
	CacheCreation1hTokens int  `json:"cache_creation_1h_tokens"`
	CacheWriteInferred    bool `json:"cache_write_inferred"`

	InputCost         float64 `json:"input_cost"`
	OutputCost        float64 `json:"output_cost"`
	CacheCreationCost float64 `json:"cache_creation_cost"`
	CacheReadCost     float64 `json:"cache_read_cost"`
	TotalCost         float64 `json:"total_cost"`
	ActualCost        float64 `json:"actual_cost"`
	RateMultiplier    float64 `json:"rate_multiplier"`
	BillingType       int8    `json:"billing_type"`
	BillingMode       *string `json:"billing_mode,omitempty"`

	DurationMs   *int `json:"duration_ms,omitempty"`
	FirstTokenMs *int `json:"first_token_ms,omitempty"`

	ImageCount         int            `json:"image_count"`
	ImageSize          *string        `json:"image_size,omitempty"`
	ImageInputSize     *string        `json:"image_input_size,omitempty"`
	ImageOutputSize    *string        `json:"image_output_size,omitempty"`
	ImageSizeSource    *string        `json:"image_size_source,omitempty"`
	ImageSizeBreakdown map[string]int `json:"image_size_breakdown,omitempty"`
	MediaType          *string        `json:"media_type,omitempty"`
	ImageOutputTokens  int            `json:"image_output_tokens"`
	ImageOutputCost    float64        `json:"image_output_cost"`
	CacheTTLOverridden bool           `json:"cache_ttl_overridden"`
}

func (s *OpsService) ListUserRequestHistory(
	ctx context.Context,
	userID int64,
	params pagination.PaginationParams,
	filter UserRequestHistoryFilter,
) ([]UserRequestRecord, int64, error) {
	if userID <= 0 {
		return nil, 0, infraerrors.BadRequest("INVALID_USER_ID", "invalid user id")
	}
	if s == nil || s.opsRepo == nil {
		return nil, 0, infraerrors.ServiceUnavailable("OPS_REPO_UNAVAILABLE", "Ops repository not available")
	}
	filter.UserID = userID
	return s.opsRepo.ListUserRequestHistory(ctx, params, filter)
}
