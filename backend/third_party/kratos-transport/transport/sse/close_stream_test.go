package sse

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func openRawStream(t *testing.T, base, id string) (*http.Response, chan struct{}) {
	t.Helper()
	resp, err := http.Get(base + "/events?stream=" + id)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	done := make(chan struct{})
	go func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		close(done)
	}()
	return resp, done
}

// 吊销全部令牌后服务端主动断开该用户已建立的 SSE 连接，其他流不受影响。
func TestCloseStreamDisconnectsSubscribers(t *testing.T) {
	s := NewServer(WithAutoStream(true))

	mux := http.NewServeMux()
	mux.HandleFunc("/events", s.ServeHTTP)
	server := httptest.NewServer(mux)

	respA, doneA := openRawStream(t, server.URL, "1")
	respB, doneB := openRawStream(t, server.URL, "2")
	// 先断客户端连接再关服务端，否则 server.Close 会一直等长连接
	defer func() {
		_ = respA.Body.Close()
		_ = respB.Body.Close()
		server.CloseClientConnections()
		server.Close()
	}()
	time.Sleep(100 * time.Millisecond)

	s.CloseStream("1")

	select {
	case <-doneA:
	case <-time.After(2 * time.Second):
		t.Fatal("CloseStream 之后连接应被服务端关闭")
	}
	select {
	case <-doneB:
		t.Fatal("其他用户的流不应被关闭")
	case <-time.After(200 * time.Millisecond):
	}

	// 重连能建立新流（不会被旧连接的清理逻辑误删）
	respA2, doneA2 := openRawStream(t, server.URL, "1")
	defer func() { _ = respA2.Body.Close() }()
	time.Sleep(200 * time.Millisecond)
	require.NotNil(t, s.streamMgr.Get("1"))
	select {
	case <-doneA2:
		t.Fatal("重连的新流不应被旧连接的清理逻辑关掉")
	default:
	}

	// 关闭不存在的流不报错
	s.CloseStream("nope")
}
