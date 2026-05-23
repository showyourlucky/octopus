package helper

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/bestruirui/octopus/internal/model"
)

func TestChannelBaseUrlDelayUpdateSkipsDisabledChannel(t *testing.T) {
	var requestCount int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requestCount, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	channel := &model.Channel{
		ID:      9,
		Name:    "disabled-channel",
		Enabled: false,
		BaseUrls: []model.BaseUrl{
			{URL: server.URL},
		},
	}

	// 禁用渠道必须在入口直接返回，避免对未启用的上游地址发起探测请求。
	ChannelBaseUrlDelayUpdate(channel, context.Background())

	if atomic.LoadInt32(&requestCount) != 0 {
		t.Fatalf("禁用渠道不应发起 URL 延迟探测请求，实际请求次数: %d", requestCount)
	}
}
