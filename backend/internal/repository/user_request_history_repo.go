package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

func userRequestTypeSQL(alias string, hasWSMode bool) string {
	fallback := fmt.Sprintf("CASE WHEN COALESCE(%s.stream, false) THEN 2 ELSE 1 END", alias)
	if hasWSMode {
		fallback = fmt.Sprintf("CASE WHEN COALESCE(%s.openai_ws_mode, false) THEN 3 WHEN COALESCE(%s.stream, false) THEN 2 ELSE 1 END", alias, alias)
	}
	return fmt.Sprintf(`CASE COALESCE(NULLIF(%[1]s.request_type, 0), %[2]s)
		WHEN 1 THEN 'sync'
		WHEN 2 THEN 'stream'
		WHEN 3 THEN 'ws_v2'
		WHEN 4 THEN 'cyber'
		ELSE 'unknown'
	END`, alias, fallback)
}

func userErrorCategorySQL(alias string) string {
	return fmt.Sprintf(`CASE
		WHEN %[1]s.error_phase = 'auth' THEN 'auth'
		WHEN %[1]s.error_phase = 'routing' THEN 'service_unavailable'
		WHEN %[1]s.error_phase IN ('upstream', 'network') THEN 'upstream'
		WHEN %[1]s.error_phase = 'internal' THEN 'internal'
		WHEN %[1]s.error_phase = 'request' AND %[1]s.error_type = 'rate_limit_error' THEN 'rate_limit'
		WHEN %[1]s.error_phase = 'request' AND %[1]s.error_type IN ('billing_error', 'subscription_error') THEN 'quota'
		WHEN %[1]s.error_phase = 'request' AND %[1]s.error_type = 'invalid_request_error' THEN 'invalid_request'
		WHEN %[1]s.error_phase = 'request' AND %[1]s.error_type = 'cyber_policy' THEN 'cyber'
		ELSE 'other'
	END`, alias)
}

func buildUserRequestHistoryCTE(filter service.UserRequestHistoryFilter) (string, []any) {
	args := []any{filter.UserID}
	successWhere := []string{"ul.user_id = $1", usageLogSuccessFilterUL}
	errorWhere := []string{
		"(e.user_id = $1 OR e.deleted_key_owner_user_id = $1)",
		"(COALESCE(e.status_code, 0) >= 400 OR e.error_type = 'cyber_policy')",
		"COALESCE(e.is_count_tokens, false) = false",
	}
	if !filter.IncludeErrors {
		errorWhere = append(errorWhere, "FALSE")
	}

	appendShared := func(value any, successCondition, errorCondition string) {
		args = append(args, value)
		placeholder := fmt.Sprintf("$%d", len(args))
		successWhere = append(successWhere, fmt.Sprintf(successCondition, placeholder))
		errorWhere = append(errorWhere, fmt.Sprintf(errorCondition, placeholder))
	}

	if filter.APIKeyID > 0 {
		appendShared(filter.APIKeyID, "ul.api_key_id = %s", "e.api_key_id = %s")
	}
	if filter.GroupID > 0 {
		appendShared(filter.GroupID, "ul.group_id = %s", "e.group_id = %s")
	}
	if model := strings.TrimSpace(filter.Model); model != "" {
		appendShared(model,
			"COALESCE(NULLIF(TRIM(ul.requested_model), ''), ul.model) = %s",
			"COALESCE(NULLIF(TRIM(e.requested_model), ''), e.model, '') = %s",
		)
	}
	if filter.RequestType != nil {
		appendShared(*filter.RequestType,
			"COALESCE(NULLIF(ul.request_type, 0), CASE WHEN COALESCE(ul.openai_ws_mode, false) THEN 3 WHEN COALESCE(ul.stream, false) THEN 2 ELSE 1 END) = %s",
			"COALESCE(NULLIF(e.request_type, 0), CASE WHEN COALESCE(e.stream, false) THEN 2 ELSE 1 END) = %s",
		)
	} else if filter.Stream != nil {
		appendShared(*filter.Stream, "COALESCE(ul.stream, false) = %s", "COALESCE(e.stream, false) = %s")
	}
	if filter.BillingType != nil {
		args = append(args, int16(*filter.BillingType))
		successWhere = append(successWhere, fmt.Sprintf("ul.billing_type = $%d", len(args)))
		errorWhere = append(errorWhere, "FALSE")
	}
	if mode := strings.TrimSpace(filter.BillingMode); mode != "" {
		var modeConditions []string
		modeConditions, args = appendUsageLogBillingModeWhereConditionWithAlias(modeConditions, args, mode, "ul")
		successWhere = append(successWhere, modeConditions...)
		errorWhere = append(errorWhere, "FALSE")
	}
	if filter.StartTime != nil && !filter.StartTime.IsZero() {
		appendShared(filter.StartTime.UTC(), "ul.created_at >= %s", "e.created_at >= %s")
	}
	if filter.EndTime != nil && !filter.EndTime.IsZero() {
		appendShared(filter.EndTime.UTC(), "ul.created_at < %s", "e.created_at < %s")
	}

	if category := strings.ToLower(strings.TrimSpace(filter.Category)); category != "" {
		if category == service.UserRequestRecordSuccess {
			errorWhere = append(errorWhere, "FALSE")
		} else {
			successWhere = append(successWhere, "FALSE")
			args = append(args, category)
			errorWhere = append(errorWhere, fmt.Sprintf("%s = $%d", userErrorCategorySQL("e"), len(args)))
		}
	}
	if filter.StatusCode != nil {
		args = append(args, *filter.StatusCode)
		placeholder := fmt.Sprintf("$%d", len(args))
		if *filter.StatusCode != 200 {
			successWhere = append(successWhere, "FALSE")
		}
		errorWhere = append(errorWhere, "COALESCE(e.upstream_status_code, e.status_code, 0) = "+placeholder)
	}

	successRequestType := userRequestTypeSQL("ul", true)
	errorRequestType := userRequestTypeSQL("e", false)
	errorCategory := userErrorCategorySQL("e")

	cte := fmt.Sprintf(`WITH request_history AS (
	SELECT
		'success'::text AS record_type,
		ul.id,
		ul.created_at,
		COALESCE(NULLIF(TRIM(ul.requested_model), ''), ul.model) AS sort_model,
		200::integer AS status_code,
		jsonb_build_object(
			'record_type', 'success',
			'id', ul.id,
			'user_id', ul.user_id,
			'created_at', ul.created_at,
			'request_id', COALESCE(ul.request_id, ''),
			'status_code', 200,
			'category', 'success',
			'model', COALESCE(NULLIF(TRIM(ul.requested_model), ''), ul.model),
			'platform', COALESCE(NULLIF(g.platform, ''), a.platform, ''),
			'message', '',
			'api_key_id', ul.api_key_id,
			'api_key', CASE WHEN ak.id IS NULL THEN NULL ELSE jsonb_build_object('id', ak.id, 'name', COALESCE(ak.name, ''), 'deleted', ak.deleted_at IS NOT NULL) END,
			'account_id', ul.account_id,
			'group_id', ul.group_id,
			'group', CASE WHEN g.id IS NULL THEN NULL ELSE jsonb_build_object('id', g.id, 'name', COALESCE(g.name, '')) END,
			'subscription_id', ul.subscription_id,
			'service_tier', ul.service_tier,
			'reasoning_effort', ul.reasoning_effort,
			'inbound_endpoint', ul.inbound_endpoint,
			'request_type', %s,
			'stream', COALESCE(ul.stream, false),
			'openai_ws_mode', COALESCE(ul.openai_ws_mode, false),
			'user_agent', ul.user_agent,
			'ip_address', NULLIF(ul.ip_address, ''),
			'input_tokens', COALESCE(ul.input_tokens, 0),
			'output_tokens', COALESCE(ul.output_tokens, 0),
			'cache_creation_tokens', COALESCE(ul.cache_creation_tokens, 0),
			'cache_read_tokens', COALESCE(ul.cache_read_tokens, 0)
		) || jsonb_build_object(
			'cache_creation_5m_tokens', COALESCE(ul.cache_creation_5m_tokens, 0),
			'cache_creation_1h_tokens', COALESCE(ul.cache_creation_1h_tokens, 0),
			'cache_write_inferred', COALESCE(ul.cache_write_inferred, false),
			'input_cost', COALESCE(ul.input_cost, 0),
			'output_cost', COALESCE(ul.output_cost, 0),
			'cache_creation_cost', COALESCE(ul.cache_creation_cost, 0),
			'cache_read_cost', COALESCE(ul.cache_read_cost, 0),
			'total_cost', COALESCE(ul.total_cost, 0),
			'actual_cost', COALESCE(ul.actual_cost, 0),
			'rate_multiplier', COALESCE(ul.rate_multiplier, 1),
			'billing_type', COALESCE(ul.billing_type, 0),
			'billing_mode', ul.billing_mode,
			'duration_ms', ul.duration_ms,
			'first_token_ms', ul.first_token_ms,
			'image_count', COALESCE(ul.image_count, 0),
			'image_size', ul.image_size,
			'image_input_size', ul.image_input_size,
			'image_output_size', ul.image_output_size,
			'image_size_source', ul.image_size_source,
			'image_size_breakdown', ul.image_size_breakdown,
			'media_type', NULL,
			'image_output_tokens', COALESCE(ul.image_output_tokens, 0),
			'image_output_cost', COALESCE(ul.image_output_cost, 0),
			'cache_ttl_overridden', COALESCE(ul.cache_ttl_overridden, false)
		) AS payload
	FROM usage_logs ul
	LEFT JOIN api_keys ak ON ak.id = ul.api_key_id
	LEFT JOIN groups g ON g.id = ul.group_id
	LEFT JOIN accounts a ON a.id = ul.account_id
	WHERE %s

	UNION ALL

	SELECT
		'error'::text AS record_type,
		e.id,
		e.created_at,
		COALESCE(NULLIF(TRIM(e.requested_model), ''), e.model, '') AS sort_model,
		COALESCE(e.upstream_status_code, e.status_code, 0)::integer AS status_code,
		jsonb_build_object(
			'record_type', 'error',
			'id', e.id,
			'user_id', $1,
			'created_at', e.created_at,
			'request_id', COALESCE(e.request_id, ''),
			'status_code', COALESCE(e.upstream_status_code, e.status_code, 0),
			'category', %s,
			'model', COALESCE(NULLIF(TRIM(e.requested_model), ''), e.model, ''),
			'platform', COALESCE(e.platform, ''),
			'message', COALESCE(e.error_message, ''),
			'api_key_id', e.api_key_id,
			'api_key', CASE WHEN ak.id IS NULL AND COALESCE(e.deleted_key_name, '') = '' THEN NULL ELSE jsonb_build_object(
				'id', COALESCE(ak.id, e.api_key_id, 0),
				'name', COALESCE(NULLIF(ak.name, ''), e.deleted_key_name, ''),
				'deleted', ak.deleted_at IS NOT NULL OR (ak.id IS NULL AND COALESCE(e.deleted_key_name, '') <> '')
			) END,
			'account_id', NULL,
			'group_id', e.group_id,
			'group', CASE WHEN g.id IS NULL THEN NULL ELSE jsonb_build_object('id', g.id, 'name', COALESCE(g.name, '')) END,
			'subscription_id', NULL,
			'service_tier', NULL,
			'reasoning_effort', NULL,
			'inbound_endpoint', NULLIF(e.inbound_endpoint, ''),
			'request_type', %s,
			'stream', COALESCE(e.stream, false),
			'openai_ws_mode', COALESCE(e.request_type, 0) = 3,
			'user_agent', NULLIF(e.user_agent, ''),
			'ip_address', CASE WHEN e.client_ip IS NULL THEN NULL ELSE host(e.client_ip) END,
			'input_tokens', 0,
			'output_tokens', 0,
			'cache_creation_tokens', 0,
			'cache_read_tokens', 0
		) || jsonb_build_object(
			'cache_creation_5m_tokens', 0,
			'cache_creation_1h_tokens', 0,
			'cache_write_inferred', false,
			'input_cost', 0,
			'output_cost', 0,
			'cache_creation_cost', 0,
			'cache_read_cost', 0,
			'total_cost', 0,
			'actual_cost', 0,
			'rate_multiplier', 0,
			'billing_type', 0,
			'billing_mode', NULL,
			'duration_ms', NULL,
			'first_token_ms', NULL,
			'image_count', 0,
			'image_size', NULL,
			'image_input_size', NULL,
			'image_output_size', NULL,
			'image_size_source', NULL,
			'image_size_breakdown', NULL,
			'media_type', NULL,
			'image_output_tokens', 0,
			'image_output_cost', 0,
			'cache_ttl_overridden', false
		) AS payload
	FROM ops_error_logs e
	LEFT JOIN api_keys ak ON ak.id = e.api_key_id
	LEFT JOIN groups g ON g.id = e.group_id
	WHERE %s
)`,
		successRequestType,
		strings.Join(successWhere, " AND "),
		errorCategory,
		errorRequestType,
		strings.Join(errorWhere, " AND "),
	)
	return cte, args
}

func userRequestHistoryOrderBy(params pagination.PaginationParams) string {
	direction := strings.ToUpper(params.NormalizedSortOrder(pagination.SortOrderDesc))
	column := "created_at"
	switch strings.ToLower(strings.TrimSpace(params.SortBy)) {
	case "model":
		column = "sort_model"
	case "status", "status_code":
		column = "status_code"
	}
	if column == "created_at" {
		return fmt.Sprintf("created_at %s, record_type %s, id %s", direction, direction, direction)
	}
	return fmt.Sprintf("%s %s, created_at %s, record_type %s, id %s", column, direction, direction, direction, direction)
}

func (r *opsRepository) ListUserRequestHistory(
	ctx context.Context,
	params pagination.PaginationParams,
	filter service.UserRequestHistoryFilter,
) ([]service.UserRequestRecord, int64, error) {
	if r == nil || r.db == nil {
		return nil, 0, fmt.Errorf("nil ops repository")
	}
	if filter.UserID <= 0 {
		return nil, 0, fmt.Errorf("invalid user id")
	}

	cte, args := buildUserRequestHistoryCTE(filter)
	var total int64
	if err := r.db.QueryRowContext(ctx, cte+" SELECT COUNT(*) FROM request_history", args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	listArgs := append(append([]any{}, args...), params.Limit(), params.Offset())
	query := fmt.Sprintf(`%s
SELECT payload
FROM request_history
ORDER BY %s
LIMIT $%d OFFSET $%d`, cte, userRequestHistoryOrderBy(params), len(args)+1, len(args)+2)
	rows, err := r.db.QueryContext(ctx, query, listArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = rows.Close() }()

	items := make([]service.UserRequestRecord, 0, params.Limit())
	for rows.Next() {
		var payload []byte
		if err := rows.Scan(&payload); err != nil {
			return nil, 0, err
		}
		var item service.UserRequestRecord
		if err := json.Unmarshal(payload, &item); err != nil {
			return nil, 0, fmt.Errorf("decode user request history row: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return items, total, nil
}
