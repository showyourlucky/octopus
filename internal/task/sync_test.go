package task

import (
	"testing"

	"github.com/bestruirui/octopus/internal/model"
)

func TestShouldSyncChannelModels(t *testing.T) {
	tests := []struct {
		name    string
		channel model.Channel
		want    bool
	}{
		{
			name: "自动同步开启且渠道已启动时允许同步",
			channel: model.Channel{
				AutoSync: true,
				Enabled:  true,
			},
			want: true,
		},
		{
			name: "自动同步开启但渠道未启动时跳过同步",
			channel: model.Channel{
				AutoSync: true,
				Enabled:  false,
			},
			want: false,
		},
		{
			name: "渠道已启动但未开启自动同步时跳过同步",
			channel: model.Channel{
				AutoSync: false,
				Enabled:  true,
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldSyncChannelModels(tt.channel); got != tt.want {
				t.Fatalf("同步判断结果不符合预期: got=%v want=%v", got, tt.want)
			}
		})
	}
}
