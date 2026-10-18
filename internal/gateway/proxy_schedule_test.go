package gateway

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"opencode2api/internal/config"
)

func testAnonymousPool() *anonymousPool {
	transports := &transportPool{}
	for i := range 3 {
		p := &proxyTransport{index: i, name: fmt.Sprintf("http://proxy%d:80", i), active: new(atomic.Int64)}
		p.healthy.Store(true)
		transports.items = append(transports.items, p)
	}
	return newAnonymousPool(true, transports, time.Second)
}

func TestAnonymousSchedulingAndCooldown(t *testing.T) {
	p := testAnonymousPool()
	a, b := p.CursorFor("session"), p.CursorFor("session")
	if a.Next() != b.Next() {
		t.Fatal("default session affinity changed")
	}
	p.strategy = "round_robin"
	a, b = p.CursorFor("session"), p.CursorFor("session")
	if a.Next() == b.Next() {
		t.Fatal("round robin still pinned to session")
	}
	p.strategy = "least_connections"
	p.nodes[0].proxy.active.Store(4)
	p.nodes[1].proxy.active.Store(2)
	p.nodes[2].proxy.active.Store(1)
	a = p.CursorFor("session")
	if a.Next() != p.nodes[2] {
		t.Fatal("least connections did not select idle route")
	}
	p.nodes[2].cooldownUntil.Store(time.Now().Add(time.Minute).UnixNano())
	a = p.CursorFor("session")
	if a.Next() != p.nodes[1] {
		t.Fatal("least connections selected a cooling route")
	}
	seen := make(map[*anonymousNode]bool)
	for node := a.Next(); node != nil; node = a.Next() {
		if seen[node] {
			t.Fatal("static route repeated during failover")
		}
		seen[node] = true
	}
}

func TestProxyConnectionsRemainActiveUntilStreamCloses(t *testing.T) {
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: hello\n\n"))
		w.(http.Flusher).Flush()
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer server.Close()
	defer close(release)
	pool, err := newTransportPool([]string{"direct"}, config.PerformanceConfig{ConnectTimeoutSeconds: 1}, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.items[0].client.CloseIdleConnections()
	request, _ := http.NewRequestWithContext(context.Background(), "GET", server.URL, nil)
	resp, err := pool.items[0].client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	if pool.items[0].active.Load() != 1 {
		t.Fatal("open SSE body must count as an active connection")
	}
	reloaded, err := newTransportPool([]string{"direct"}, config.PerformanceConfig{ConnectTimeoutSeconds: 1}, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer reloaded.items[0].client.CloseIdleConnections()
	reuseProxyConnectionCounters(pool, reloaded)
	if reloaded.items[0].active.Load() != 1 {
		t.Fatal("hot reload lost existing stream")
	}
	second, err := reloaded.items[0].client.Get(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.items[0].active.Load() != 2 {
		t.Fatal("old and new runtimes must share a connection count")
	}
	_ = second.Body.Close()
	_ = resp.Body.Close()
	_ = resp.Body.Close()
	if pool.items[0].active.Load() != 0 || reloaded.items[0].active.Load() != 0 {
		t.Fatal("response close did not release exactly once")
	}
}
