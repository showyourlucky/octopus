package model

import "testing"

func TestNormalizeKeyLoadBalanceMode(t *testing.T) {
	tests := []struct {
		name string
		mode string
		want string
	}{
		{name: "空值默认故障转移", mode: "", want: KeyLoadBalanceModeFailover},
		{name: "未知值默认故障转移", mode: "unknown", want: KeyLoadBalanceModeFailover},
		{name: "保留故障转移", mode: KeyLoadBalanceModeFailover, want: KeyLoadBalanceModeFailover},
		{name: "保留轮询", mode: KeyLoadBalanceModeRoundRobin, want: KeyLoadBalanceModeRoundRobin},
		{name: "保留随机", mode: KeyLoadBalanceModeRandom, want: KeyLoadBalanceModeRandom},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NormalizeKeyLoadBalanceMode(tt.mode); got != tt.want {
				t.Fatalf("NormalizeKeyLoadBalanceMode(%q) = %q, want %q", tt.mode, got, tt.want)
			}
		})
	}
}

func TestGetCandidateKeysFailoverSortsByKeyID(t *testing.T) {
	channel := Channel{
		KeyLoadBalanceMode: KeyLoadBalanceModeFailover,
		Keys: []ChannelKey{
			{ID: 30, Enabled: true, ChannelKey: "key-30"},
			{ID: 10, Enabled: true, ChannelKey: "key-10"},
			{ID: 20, Enabled: true, ChannelKey: "key-20"},
		},
	}

	got := channel.GetCandidateKeys()
	assertKeyIDs(t, got, []int{10, 20, 30})
}

func TestGetCandidateKeysRoundRobinSortsByLastUseThenID(t *testing.T) {
	channel := Channel{
		KeyLoadBalanceMode: KeyLoadBalanceModeRoundRobin,
		Keys: []ChannelKey{
			{ID: 30, Enabled: true, ChannelKey: "key-30", LastUseTimeStamp: 200},
			{ID: 20, Enabled: true, ChannelKey: "key-20", LastUseTimeStamp: 100},
			{ID: 10, Enabled: true, ChannelKey: "key-10", LastUseTimeStamp: 100},
		},
	}

	got := channel.GetCandidateKeys()
	assertKeyIDs(t, got, []int{10, 20, 30})
}

func assertKeyIDs(t *testing.T, keys []ChannelKey, want []int) {
	t.Helper()
	if len(keys) != len(want) {
		t.Fatalf("候选 Key 数量 = %d, want %d", len(keys), len(want))
	}
	for i := range want {
		if keys[i].ID != want[i] {
			t.Fatalf("候选 Key[%d].ID = %d, want %d", i, keys[i].ID, want[i])
		}
	}
}
