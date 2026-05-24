package op

import (
	"context"
	"testing"

	"github.com/bestruirui/octopus/internal/model"
)

func TestAPIKeyVisibleModelsAllChannelSkipsDisabledChannelModels(t *testing.T) {
	resetAPIKeyVisibleModelsTestCache()
	t.Cleanup(resetAPIKeyVisibleModelsTestCache)

	groupCache.Set(1, model.Group{ID: 1, Name: "group-model"})
	channelCache.Set(1, model.Channel{
		ID:          1,
		Name:        "enabled-channel",
		Enabled:     true,
		Model:       "enabled-model,shared-model",
		CustomModel: "enabled-custom-model",
	})
	channelCache.Set(2, model.Channel{
		ID:          2,
		Name:        "disabled-channel",
		Enabled:     false,
		Model:       "disabled-only-model,shared-model",
		CustomModel: "disabled-custom-model",
	})

	models, err := APIKeyVisibleModels(model.APIKey{
		ModelAccessType: model.APIKeyModelAccessTypeAllChannel,
	}, context.Background())
	if err != nil {
		t.Fatalf("APIKeyVisibleModels() error = %v", err)
	}

	want := []string{"enabled-custom-model", "enabled-model", "group-model", "shared-model"}
	if len(models) != len(want) {
		t.Fatalf("APIKeyVisibleModels() = %v, want %v", models, want)
	}
	for i := range want {
		if models[i] != want[i] {
			t.Fatalf("APIKeyVisibleModels() = %v, want %v", models, want)
		}
	}
}

func resetAPIKeyVisibleModelsTestCache() {
	// 这些缓存是 op 包内的全局状态，测试前后清理可以避免用例互相污染。
	groupCache.Clear()
	groupMap.Clear()
	channelCache.Clear()
	channelKeyCache.Clear()
}
