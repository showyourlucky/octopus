package model

import "testing"

func TestNormalizeAPIKeyModelAccessType(t *testing.T) {
	tests := []struct {
		name       string
		accessType string
		want       string
	}{
		{name: "空值默认分组模型", accessType: "", want: APIKeyModelAccessTypeGroup},
		{name: "未知值默认分组模型", accessType: "unknown", want: APIKeyModelAccessTypeGroup},
		{name: "保留分组模型", accessType: APIKeyModelAccessTypeGroup, want: APIKeyModelAccessTypeGroup},
		{name: "保留全渠道模型", accessType: APIKeyModelAccessTypeAllChannel, want: APIKeyModelAccessTypeAllChannel},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NormalizeAPIKeyModelAccessType(tt.accessType); got != tt.want {
				t.Fatalf("NormalizeAPIKeyModelAccessType(%q) = %q, want %q", tt.accessType, got, tt.want)
			}
		})
	}
}
