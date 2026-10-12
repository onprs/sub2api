//go:build unit

package admin

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestOpenCodeAccountModelListRequiresPublishedPrices(t *testing.T) {
	account := &service.Account{Platform: service.PlatformOpenCodeGo, Type: service.AccountTypeAPIKey,
		Credentials: map[string]any{"model_mapping": map[string]any{"unpriced-model": "omen-alpha", "priced-alias": "glm-5.3"}}}
	models := openCodeGoAvailableModels(account)
	require.Len(t, models, 1)
	require.Equal(t, "priced-alias", models[0].ID)
	account.Credentials["model_mapping"] = map[string]any{"unpriced-model": "omen-alpha"}
	require.Empty(t, openCodeGoAvailableModels(account))
	delete(account.Credentials, "model_mapping")
	require.Len(t, openCodeGoAvailableModels(account), len(service.DefaultOpenCodeGoModelIDs()))
	account.Credentials["account_mode"] = service.AccountModeZen
	models = openCodeGoAvailableModels(account)
	ids := make([]string, 0, len(models))
	for _, model := range models {
		ids = append(ids, model.ID)
	}
	require.Contains(t, ids, "claude-fable-5-1")
	require.NotContains(t, ids, "omen-alpha")
	require.NotContains(t, ids, "longcat-2.0", "Zen 目录不能混入仅在 Go 公布价格的模型")
}
