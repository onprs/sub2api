package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type channelMonitorTargetRepoStub struct {
	ChannelMonitorRepository
	created           *ChannelMonitor
	existing          *ChannelMonitor
	pending           []*ChannelMonitor
	resolvedGroups    map[string]int64
	migratedTargets   []ChannelMonitorLocalTargetMigration
	migrateCalls      int
	enabledLocal      []*ChannelMonitor
	latest            map[int64][]*ChannelMonitorLatest
	availability      map[int64][]*ChannelMonitorAvailability
	listLocalCalls    int
	latestCalls       int
	availabilityCalls int
}

func (r *channelMonitorTargetRepoStub) Create(_ context.Context, monitor *ChannelMonitor) error {
	copy := *monitor
	r.created = &copy
	monitor.ID = 1001
	return nil
}

func (r *channelMonitorTargetRepoStub) GetByID(_ context.Context, _ int64) (*ChannelMonitor, error) {
	if r.existing == nil {
		return nil, ErrChannelMonitorNotFound
	}
	copy := *r.existing
	return &copy, nil
}

func (r *channelMonitorTargetRepoStub) Update(_ context.Context, monitor *ChannelMonitor) error {
	copy := *monitor
	r.existing = &copy
	return nil
}

func (r *channelMonitorTargetRepoStub) ListPendingLocalTargets(context.Context) ([]*ChannelMonitor, error) {
	return append([]*ChannelMonitor(nil), r.pending...), nil
}

func (r *channelMonitorTargetRepoStub) ResolveLegacyAPIKeyGroupID(_ context.Context, apiKey string) (int64, error) {
	groupID, ok := r.resolvedGroups[apiKey]
	if !ok {
		return 0, errors.New("api key mapping not found")
	}
	return groupID, nil
}

func (r *channelMonitorTargetRepoStub) MigrateLocalTargets(_ context.Context, targets []ChannelMonitorLocalTargetMigration) error {
	r.migrateCalls++
	r.migratedTargets = append([]ChannelMonitorLocalTargetMigration(nil), targets...)
	return nil
}

func (r *channelMonitorTargetRepoStub) ListEnabledLocalByGroupIDs(context.Context, []int64) ([]*ChannelMonitor, error) {
	r.listLocalCalls++
	return append([]*ChannelMonitor(nil), r.enabledLocal...), nil
}

func (r *channelMonitorTargetRepoStub) ListLatestForMonitorIDs(context.Context, []int64) (map[int64][]*ChannelMonitorLatest, error) {
	r.latestCalls++
	return r.latest, nil
}

func (r *channelMonitorTargetRepoStub) ComputeAvailabilityForMonitors(context.Context, []int64, int) (map[int64][]*ChannelMonitorAvailability, error) {
	r.availabilityCalls++
	return r.availability, nil
}

type channelMonitorTargetRuntimeStub struct{}

func (channelMonitorTargetRuntimeStub) GetChannelMonitorRuntime(context.Context) ChannelMonitorRuntime {
	return ChannelMonitorRuntime{Enabled: true, Mode: ChannelMonitorModeV1}
}

type blockingChannelMonitorHealthRepo struct {
	ChannelMonitorRepository
	started chan struct{}
	release chan struct{}
}

func (r *blockingChannelMonitorHealthRepo) ListEnabledLocalByGroupIDs(ctx context.Context, _ []int64) ([]*ChannelMonitor, error) {
	close(r.started)
	select {
	case <-r.release:
		return []*ChannelMonitor{}, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

type channelMonitorTargetGroupRepoStub struct {
	GroupRepository
	groups map[int64]*Group
}

func (r *channelMonitorTargetGroupRepoStub) GetByIDLite(_ context.Context, id int64) (*Group, error) {
	group := r.groups[id]
	if group == nil {
		return nil, ErrGroupNotFound
	}
	copy := *group
	return &copy, nil
}

type channelMonitorTargetEncryptorStub struct {
	decryptErrors map[string]error
}

func (e *channelMonitorTargetEncryptorStub) Encrypt(plaintext string) (string, error) {
	return "encrypted:" + plaintext, nil
}

func (e *channelMonitorTargetEncryptorStub) Decrypt(ciphertext string) (string, error) {
	if err := e.decryptErrors[ciphertext]; err != nil {
		return "", err
	}
	return strings.TrimPrefix(ciphertext, "encrypted:"), nil
}

func activeMonitorTargetGroup(id int64, name, platform string) *Group {
	return &Group{ID: id, Name: name, Platform: platform, Status: StatusActive}
}

func validMonitorCreateParams() ChannelMonitorCreateParams {
	return ChannelMonitorCreateParams{
		Name:             "primary route",
		Provider:         MonitorProviderOpenAI,
		APIMode:          MonitorAPIModeChatCompletions,
		PrimaryModel:     "gpt-5.4",
		IntervalSeconds:  60,
		BodyOverrideMode: MonitorBodyOverrideModeOff,
	}
}

func TestChannelMonitorLocalBaseEndpointSupportsAllOpenCodeGoModes(t *testing.T) {
	base := channelMonitorLocalBaseEndpoint(MonitorProviderOpenCodeGo)
	require.Equal(t, channelMonitorLocalEndpoint+"/v1", base)
	for _, mode := range []string{
		MonitorAPIModeChatCompletions,
		MonitorAPIModeResponses,
		MonitorAPIModeMessages,
	} {
		adapter, _, ok := providerAdapterFor(MonitorProviderOpenCodeGo, mode)
		require.True(t, ok)
		require.Contains(t, []string{
			base + "/chat/completions",
			base + "/responses",
			base + "/messages",
		}, joinURL(base, adapter.buildPath("test-model")))
	}
	require.Equal(t, channelMonitorLocalEndpoint, channelMonitorLocalBaseEndpoint(MonitorProviderAnthropic))
}

func TestChannelMonitorLocalOpenCodeGoMessagesUsesInProcessGateway(t *testing.T) {
	groupID := int64(28)
	fallbackID := int64(29)
	group := activeMonitorTargetGroup(groupID, "OpenCode Go", PlatformOpenCodeGo)
	group.FallbackGroupID = &fallbackID
	group.FallbackGroupIDOnInvalidRequest = &fallbackID
	monitor := &ChannelMonitor{
		ID:           46,
		Provider:     MonitorProviderOpenCodeGo,
		TargetType:   ChannelMonitorTargetLocal,
		GroupID:      &groupID,
		Group:        group,
		PrimaryModel: "muse-spark-1.2-contributor",
	}
	svc := NewChannelMonitorService(&channelMonitorTargetRepoStub{}, &channelMonitorTargetEncryptorStub{})
	svc.SetLocalRequestHandler(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		require.Equal(t, "/v1/messages", request.URL.Path)
		require.Equal(t, "sub2api.internal", request.Host)
		apiKey, ok := InternalChannelMonitorAPIKey(request.Context())
		require.True(t, ok)
		require.Equal(t, groupID, *apiKey.GroupID)
		require.Nil(t, apiKey.Group.FallbackGroupID)
		require.Nil(t, apiKey.Group.FallbackGroupIDOnInvalidRequest)

		var payload struct {
			Messages []struct {
				Content string `json:"content"`
			} `json:"messages"`
		}
		require.NoError(t, json.NewDecoder(request.Body).Decode(&payload))
		require.Len(t, payload.Messages, 1)
		checkCode := regexp.MustCompile(`\d{6}`).FindString(payload.Messages[0].Content)
		require.NotEmpty(t, checkCode)
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(map[string]any{
			"content": []map[string]string{{"type": "text", "text": checkCode}},
		}))
	}))

	result := svc.runLocalCheckForModel(context.Background(), monitor, monitor.PrimaryModel, &CheckOptions{
		APIMode: MonitorAPIModeMessages,
	})
	require.Equal(t, MonitorStatusOperational, result.Status, result.Message)
}

func TestChannelMonitorLocalCommandCodeSupportsChatAndMessages(t *testing.T) {
	groupID := int64(38)
	group := activeMonitorTargetGroup(groupID, "Command Code", PlatformCommandCode)
	svc := NewChannelMonitorService(&channelMonitorTargetRepoStub{}, &channelMonitorTargetEncryptorStub{})

	// 1. Chat Completions
	chatMonitor := &ChannelMonitor{
		ID:           47,
		Provider:     MonitorProviderCommandCode,
		APIMode:      MonitorAPIModeChatCompletions,
		TargetType:   ChannelMonitorTargetLocal,
		GroupID:      &groupID,
		Group:        group,
		PrimaryModel: "deepseek-v4-pro",
	}
	svc.SetLocalRequestHandler(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		require.Equal(t, "/provider/v1/chat/completions", request.URL.Path)
		require.Equal(t, "sub2api.internal", request.Host)
		apiKey, ok := InternalChannelMonitorAPIKey(request.Context())
		require.True(t, ok)
		require.Equal(t, groupID, *apiKey.GroupID)

		var payload struct {
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		require.NoError(t, json.NewDecoder(request.Body).Decode(&payload))
		userContent := ""
		for _, msg := range payload.Messages {
			if msg.Role == "user" {
				userContent = msg.Content
			}
		}
		checkCode := regexp.MustCompile(`\d{6}`).FindString(userContent)
		require.NotEmpty(t, checkCode)
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]string{"role": "assistant", "content": checkCode}},
			},
		}))
	}))
	chatResult := svc.runLocalCheckForModel(context.Background(), chatMonitor, chatMonitor.PrimaryModel, &CheckOptions{
		APIMode: MonitorAPIModeChatCompletions,
	})
	require.Equal(t, MonitorStatusOperational, chatResult.Status, chatResult.Message)

	// 2. Messages
	messagesMonitor := &ChannelMonitor{
		ID:           48,
		Provider:     MonitorProviderCommandCode,
		APIMode:      MonitorAPIModeMessages,
		TargetType:   ChannelMonitorTargetLocal,
		GroupID:      &groupID,
		Group:        group,
		PrimaryModel: "claude-sonnet-5",
	}
	svc.SetLocalRequestHandler(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		require.Equal(t, "/provider/v1/messages", request.URL.Path)
		require.Equal(t, "sub2api.internal", request.Host)
		apiKey, ok := InternalChannelMonitorAPIKey(request.Context())
		require.True(t, ok)
		require.Equal(t, groupID, *apiKey.GroupID)

		var payload struct {
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		require.NoError(t, json.NewDecoder(request.Body).Decode(&payload))
		userContent := ""
		for _, msg := range payload.Messages {
			if msg.Role == "user" || msg.Role == "" {
				userContent = msg.Content
			}
		}
		checkCode := regexp.MustCompile(`\d{6}`).FindString(userContent)
		require.NotEmpty(t, checkCode)
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(map[string]any{
			"content": []map[string]string{{"type": "text", "text": checkCode}},
		}))
	}))
	messagesResult := svc.runLocalCheckForModel(context.Background(), messagesMonitor, messagesMonitor.PrimaryModel, &CheckOptions{
		APIMode: MonitorAPIModeMessages,
	})
	require.Equal(t, MonitorStatusOperational, messagesResult.Status, messagesResult.Message)
}

func TestChannelMonitorCreateLocalBindsGroupWithoutCredentials(t *testing.T) {
	groupID := int64(20)
	repo := &channelMonitorTargetRepoStub{}
	groupRepo := &channelMonitorTargetGroupRepoStub{groups: map[int64]*Group{
		groupID: activeMonitorTargetGroup(groupID, "OpenAI Primary", PlatformOpenAI),
	}}
	svc := NewChannelMonitorService(repo, &channelMonitorTargetEncryptorStub{}, groupRepo)
	params := validMonitorCreateParams()
	params.TargetType = ChannelMonitorTargetLocal
	params.GroupID = &groupID
	params.Endpoint = "https://should-not-be-stored.example"
	params.APIKey = "should-not-be-stored"

	monitor, err := svc.Create(context.Background(), params)
	require.NoError(t, err)
	require.Equal(t, ChannelMonitorTargetLocal, monitor.TargetType)
	require.Equal(t, groupID, *monitor.GroupID)
	require.Equal(t, "OpenAI Primary", monitor.GroupName)
	require.Empty(t, monitor.Endpoint)
	require.Empty(t, monitor.APIKey)
	require.NotNil(t, repo.created)
	require.Empty(t, repo.created.Endpoint)
	require.Empty(t, repo.created.APIKey)
}

func TestChannelMonitorCreateLegacyExternalEncryptsCredentials(t *testing.T) {
	repo := &channelMonitorTargetRepoStub{}
	svc := NewChannelMonitorService(repo, &channelMonitorTargetEncryptorStub{})
	params := validMonitorCreateParams()
	params.Provider = MonitorProviderClinePass
	params.Endpoint = "https://1.1.1.1/v1/"
	params.APIKey = "external-secret"

	monitor, err := svc.Create(context.Background(), params)
	require.NoError(t, err)
	require.Equal(t, ChannelMonitorTargetExternal, monitor.TargetType)
	require.Nil(t, monitor.GroupID)
	require.Equal(t, "https://1.1.1.1/v1", monitor.Endpoint)
	require.Equal(t, "external-secret", monitor.APIKey)
	require.Equal(t, "encrypted:external-secret", repo.created.APIKey)
}

func TestChannelMonitorUpdateExternalProviderRequiresNewAPIKey(t *testing.T) {
	repo := &channelMonitorTargetRepoStub{existing: &ChannelMonitor{
		ID:               44,
		Name:             "External Route",
		Provider:         MonitorProviderOpenAI,
		APIMode:          MonitorAPIModeChatCompletions,
		TargetType:       ChannelMonitorTargetExternal,
		Endpoint:         "https://1.1.1.1",
		APIKey:           "encrypted:old-secret",
		PrimaryModel:     "gpt-5.4",
		IntervalSeconds:  60,
		BodyOverrideMode: MonitorBodyOverrideModeOff,
	}}
	svc := NewChannelMonitorService(repo, &channelMonitorTargetEncryptorStub{})
	provider := MonitorProviderClinePass

	_, err := svc.Update(context.Background(), 44, ChannelMonitorUpdateParams{Provider: &provider})
	require.ErrorIs(t, err, ErrChannelMonitorMissingAPIKey)
	require.Equal(t, MonitorProviderOpenAI, repo.existing.Provider)

	apiKey := "new-secret"
	monitor, err := svc.Update(context.Background(), 44, ChannelMonitorUpdateParams{
		Provider: &provider,
		APIKey:   &apiKey,
	})
	require.NoError(t, err)
	require.Equal(t, MonitorProviderClinePass, monitor.Provider)
	require.Equal(t, "new-secret", monitor.APIKey)
	require.Equal(t, "encrypted:new-secret", repo.existing.APIKey)
}

func TestChannelMonitorCreateLocalRejectsIncompatibleGroup(t *testing.T) {
	groupID := int64(20)
	repo := &channelMonitorTargetRepoStub{}
	groupRepo := &channelMonitorTargetGroupRepoStub{groups: map[int64]*Group{
		groupID: activeMonitorTargetGroup(groupID, "Anthropic Primary", PlatformAnthropic),
	}}
	svc := NewChannelMonitorService(repo, &channelMonitorTargetEncryptorStub{}, groupRepo)
	params := validMonitorCreateParams()
	params.TargetType = ChannelMonitorTargetLocal
	params.GroupID = &groupID

	_, err := svc.Create(context.Background(), params)
	require.ErrorIs(t, err, ErrChannelMonitorGroupPlatformMismatch)
	require.Nil(t, repo.created)
}

func TestChannelMonitorRoutingHealthRefreshDoesNotBlockConcurrentRouting(t *testing.T) {
	repo := &blockingChannelMonitorHealthRepo{
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
	svc := NewChannelMonitorService(repo, &channelMonitorTargetEncryptorStub{})
	firstDone := make(chan struct{})
	go func() {
		_ = svc.GetAPIKeyRoutingHealthSnapshots(context.Background(), []int64{20})
		close(firstDone)
	}()
	<-repo.started

	secondResult := make(chan []APIKeyRoutingHealthSnapshot, 1)
	go func() {
		secondResult <- svc.GetAPIKeyRoutingHealthSnapshots(context.Background(), []int64{21})
	}()
	select {
	case snapshots := <-secondResult:
		require.Len(t, snapshots, 1)
		require.Equal(t, APIKeyRoutingHealthStatusUnknown, snapshots[0].Status)
	case <-time.After(time.Second):
		t.Fatal("concurrent routing health read waited for database refresh")
	}

	close(repo.release)
	select {
	case <-firstDone:
	case <-time.After(time.Second):
		t.Fatal("routing health refresh did not finish")
	}
}

func TestChannelMonitorRunCheckRejectsDisabledLocalGroup(t *testing.T) {
	groupID := int64(20)
	repo := &channelMonitorTargetRepoStub{existing: &ChannelMonitor{
		ID:              34,
		Provider:        MonitorProviderOpenAI,
		TargetType:      ChannelMonitorTargetLocal,
		GroupID:         &groupID,
		Group:           &Group{ID: groupID, Platform: PlatformOpenAI, Status: StatusDisabled},
		PrimaryModel:    "gpt-5.4",
		IntervalSeconds: 60,
	}}
	svc := NewChannelMonitorService(repo, &channelMonitorTargetEncryptorStub{})
	svc.SetRuntimeReader(channelMonitorTargetRuntimeStub{})

	_, err := svc.RunCheck(context.Background(), 34)
	require.ErrorIs(t, err, ErrChannelMonitorGroupUnavailable)
}

func TestMigrateLegacyLocalTargetsResolvesAllBeforeWriting(t *testing.T) {
	repo := &channelMonitorTargetRepoStub{
		pending: []*ChannelMonitor{
			{ID: 34, Provider: MonitorProviderOpenAI, APIKey: "encrypted:key-a"},
			{ID: 37, Provider: MonitorProviderAnthropic, APIKey: "encrypted:key-b"},
		},
		resolvedGroups: map[string]int64{"key-a": 20, "key-b": 21},
	}
	groupRepo := &channelMonitorTargetGroupRepoStub{groups: map[int64]*Group{
		20: activeMonitorTargetGroup(20, "OpenAI Primary", PlatformOpenAI),
		21: activeMonitorTargetGroup(21, "Anthropic Primary", PlatformAnthropic),
	}}
	svc := NewChannelMonitorService(repo, &channelMonitorTargetEncryptorStub{}, groupRepo)

	err := svc.MigrateLegacyLocalTargets(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, repo.migrateCalls)
	require.Equal(t, []ChannelMonitorLocalTargetMigration{
		{MonitorID: 34, GroupID: 20, GroupName: "OpenAI Primary"},
		{MonitorID: 37, GroupID: 21, GroupName: "Anthropic Primary"},
	}, repo.migratedTargets)
}

func TestMigrateLegacyLocalTargetsDoesNotPartiallyWrite(t *testing.T) {
	repo := &channelMonitorTargetRepoStub{
		pending: []*ChannelMonitor{
			{ID: 34, Provider: MonitorProviderOpenAI, APIKey: "encrypted:key-a"},
			{ID: 37, Provider: MonitorProviderAnthropic, APIKey: "broken-ciphertext"},
		},
		resolvedGroups: map[string]int64{"key-a": 20},
	}
	groupRepo := &channelMonitorTargetGroupRepoStub{groups: map[int64]*Group{
		20: activeMonitorTargetGroup(20, "OpenAI Primary", PlatformOpenAI),
	}}
	encryptor := &channelMonitorTargetEncryptorStub{decryptErrors: map[string]error{
		"broken-ciphertext": errors.New("decrypt failed"),
	}}
	svc := NewChannelMonitorService(repo, encryptor, groupRepo)

	err := svc.MigrateLegacyLocalTargets(context.Background())
	require.ErrorContains(t, err, "decrypt legacy channel monitor 37")
	require.Zero(t, repo.migrateCalls)
	require.Empty(t, repo.migratedTargets)
}

func TestMigrateLegacyLocalTargetsRejectsDuplicateGroup(t *testing.T) {
	repo := &channelMonitorTargetRepoStub{
		pending: []*ChannelMonitor{
			{ID: 34, Provider: MonitorProviderOpenAI, APIKey: "encrypted:key-a"},
			{ID: 35, Provider: MonitorProviderOpenAI, APIKey: "encrypted:key-b"},
		},
		resolvedGroups: map[string]int64{"key-a": 20, "key-b": 20},
	}
	groupRepo := &channelMonitorTargetGroupRepoStub{groups: map[int64]*Group{
		20: activeMonitorTargetGroup(20, "OpenAI Primary", PlatformOpenAI),
	}}
	svc := NewChannelMonitorService(repo, &channelMonitorTargetEncryptorStub{}, groupRepo)

	err := svc.MigrateLegacyLocalTargets(context.Background())
	require.ErrorContains(t, err, "resolve to the same group 20")
	require.Zero(t, repo.migrateCalls)
}

func TestChannelMonitorRoutingHealthIgnoresDisabledMonitor(t *testing.T) {
	groupID := int64(20)
	repo := &channelMonitorTargetRepoStub{
		enabledLocal: []*ChannelMonitor{{
			ID:           34,
			Provider:     MonitorProviderOpenAI,
			TargetType:   ChannelMonitorTargetLocal,
			GroupID:      &groupID,
			Group:        activeMonitorTargetGroup(groupID, "OpenAI Primary", PlatformOpenAI),
			Enabled:      false,
			PrimaryModel: "gpt-5.4",
		}},
	}
	svc := NewChannelMonitorService(repo, &channelMonitorTargetEncryptorStub{})

	snapshots := svc.GetAPIKeyRoutingHealthSnapshots(context.Background(), []int64{groupID})
	require.Len(t, snapshots, 1)
	require.Equal(t, APIKeyRoutingHealthStatusUnknown, snapshots[0].Status)
	require.Zero(t, repo.latestCalls)
	require.Zero(t, repo.availabilityCalls)
}

func TestChannelMonitorRoutingHealthIgnoresInactiveTargetGroup(t *testing.T) {
	groupID := int64(20)
	disabledGroup := activeMonitorTargetGroup(groupID, "OpenAI Primary", PlatformOpenAI)
	disabledGroup.Status = StatusDisabled
	repo := &channelMonitorTargetRepoStub{
		enabledLocal: []*ChannelMonitor{{
			ID:           34,
			Provider:     MonitorProviderOpenAI,
			TargetType:   ChannelMonitorTargetLocal,
			GroupID:      &groupID,
			Group:        disabledGroup,
			Enabled:      true,
			PrimaryModel: "gpt-5.4",
		}},
	}
	svc := NewChannelMonitorService(repo, &channelMonitorTargetEncryptorStub{})

	snapshots := svc.GetAPIKeyRoutingHealthSnapshots(context.Background(), []int64{groupID})
	require.Len(t, snapshots, 1)
	require.Equal(t, APIKeyRoutingHealthStatusUnknown, snapshots[0].Status)
	require.Zero(t, repo.latestCalls)
	require.Zero(t, repo.availabilityCalls)
}

func TestChannelMonitorRoutingHealthUsesPrimaryModelSevenDayData(t *testing.T) {
	groupID := int64(20)
	otherGroupID := int64(21)
	latency := 321
	observedAt := time.Date(2026, 8, 20, 12, 0, 0, 0, time.FixedZone("test", 8*60*60))
	repo := &channelMonitorTargetRepoStub{
		enabledLocal: []*ChannelMonitor{{
			ID:           34,
			Provider:     MonitorProviderOpenAI,
			GroupID:      &groupID,
			Group:        activeMonitorTargetGroup(groupID, "OpenAI Primary", PlatformOpenAI),
			TargetType:   ChannelMonitorTargetLocal,
			Enabled:      true,
			PrimaryModel: "gpt-5.4",
		}},
		latest: map[int64][]*ChannelMonitorLatest{
			34: {
				{Model: "secondary-model", Status: MonitorStatusFailed, CheckedAt: observedAt.Add(time.Minute)},
				{Model: "gpt-5.4", Status: MonitorStatusDegraded, CheckedAt: observedAt},
			},
		},
		availability: map[int64][]*ChannelMonitorAvailability{
			34: {
				{Model: "gpt-5.4", WindowDays: 7, TotalChecks: 42, AvailabilityPct: 97.5, AvgLatencyMs: &latency},
			},
		},
	}
	svc := NewChannelMonitorService(repo, &channelMonitorTargetEncryptorStub{})

	snapshots := svc.GetAPIKeyRoutingHealthSnapshots(context.Background(), []int64{groupID, otherGroupID})
	require.Len(t, snapshots, 2)
	require.Equal(t, APIKeyRoutingHealthStatusDegraded, snapshots[0].Status)
	require.Equal(t, 97.5, *snapshots[0].SuccessRate)
	require.Equal(t, int64(321), *snapshots[0].AverageLatencyMs)
	require.Equal(t, int64(42), snapshots[0].SampleCount)
	require.Equal(t, observedAt.UTC(), *snapshots[0].LastObservedAt)
	require.Equal(t, APIKeyRoutingHealthStatusUnknown, snapshots[1].Status)
	require.Nil(t, snapshots[1].SuccessRate)

	cached := svc.GetAPIKeyRoutingHealthSnapshots(context.Background(), []int64{groupID, otherGroupID})
	require.Equal(t, snapshots, cached)
	require.Equal(t, 1, repo.listLocalCalls)
	require.Equal(t, 1, repo.latestCalls)
	require.Equal(t, 1, repo.availabilityCalls)

	svc.invalidateAPIKeyRoutingHealth(groupID)
	_ = svc.GetAPIKeyRoutingHealthSnapshots(context.Background(), []int64{groupID, otherGroupID})
	require.Equal(t, 2, repo.listLocalCalls)
	require.Equal(t, 2, repo.latestCalls)
	require.Equal(t, 2, repo.availabilityCalls)
}
