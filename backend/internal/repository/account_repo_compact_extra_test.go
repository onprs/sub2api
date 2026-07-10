package repository

import "testing"

func TestShouldEnqueueSchedulerOutboxForExtraUpdates_CompactCapabilityKeysAreRelevant(t *testing.T) {
	updates := map[string]any{
		"openai_compact_supported":  true,
		"openai_compact_checked_at": "2026-04-10T10:00:00Z",
	}

	if !shouldEnqueueSchedulerOutboxForExtraUpdates(updates) {
		t.Fatalf("expected compact capability updates to enqueue scheduler outbox")
	}
}

func TestShouldEnqueueSchedulerOutboxForExtraUpdates_OpenAIResponsesCapabilityKeysAreRelevant(t *testing.T) {
	updates := map[string]any{
		"openai_responses_mode":      "force_chat_completions",
		"openai_responses_supported": false,
	}

	if !shouldEnqueueSchedulerOutboxForExtraUpdates(updates) {
		t.Fatalf("expected responses capability updates to enqueue scheduler outbox")
	}
}

func TestShouldEnqueueSchedulerOutboxForExtraUpdates_OpenCodeGoUsageSnapshotIsNeutral(t *testing.T) {
	updates := map[string]any{
		"opencode_go_usage_updated_at":        "2026-06-18T10:00:00Z",
		"opencode_go_usage_source":            "estimated",
		"opencode_go_usage_5h_used_percent":   42.0,
		"opencode_go_usage_7d_used_percent":   55.0,
		"opencode_go_usage_30d_used_percent":  60.0,
		"opencode_go_usage_30d_limit_usd":     60.0,
		"opencode_go_usage_30d_account_cost":  36.0,
		"opencode_go_usage_30d_standard_cost": 35.0,
		"opencode_go_usage_30d_user_cost":     40.0,
	}

	if shouldEnqueueSchedulerOutboxForExtraUpdates(updates) {
		t.Fatalf("expected OpenCode Go usage snapshot updates to be scheduler-neutral")
	}
}
