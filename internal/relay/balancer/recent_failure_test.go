package balancer

import (
	"testing"

	"github.com/bestruirui/octopus/internal/model"
)

func TestSortRecentFailuresLast(t *testing.T) {
	modelName := "gpt-test"
	defer ClearRecentModelFailure(1, modelName)
	defer ClearRecentModelFailure(2, modelName)

	RecordRecentModelFailure(1, modelName)

	items := []model.GroupItem{
		{ChannelID: 1, ModelName: modelName, Priority: 1},
		{ChannelID: 2, ModelName: modelName, Priority: 2},
	}

	got := sortRecentFailuresLast(items, modelName)
	if len(got) != 2 {
		t.Fatalf("候选数量 = %d，want 2", len(got))
	}
	if got[0].ChannelID != 2 || got[1].ChannelID != 1 {
		t.Fatalf("近期失败渠道未排到后面，got channel order [%d, %d]", got[0].ChannelID, got[1].ChannelID)
	}
}

func TestClearRecentModelFailure(t *testing.T) {
	modelName := "gpt-test-clear"
	defer ClearRecentModelFailure(1, modelName)

	RecordRecentModelFailure(1, modelName)
	ClearRecentModelFailure(1, modelName)

	items := []model.GroupItem{
		{ChannelID: 1, ModelName: modelName, Priority: 1},
		{ChannelID: 2, ModelName: modelName, Priority: 2},
	}

	got := sortRecentFailuresLast(items, modelName)
	if got[0].ChannelID != 1 || got[1].ChannelID != 2 {
		t.Fatalf("清理近期失败后顺序应恢复优先级，got channel order [%d, %d]", got[0].ChannelID, got[1].ChannelID)
	}
}
