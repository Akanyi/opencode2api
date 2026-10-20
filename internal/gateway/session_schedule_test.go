package gateway

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"opencode2api/internal/config"
	"opencode2api/internal/telemetry"
)

func TestStickyRoundRobinBalancesConcurrentSessions(t *testing.T) {
	p := testAnonymousPool()
	p.strategy = "sticky_round_robin"
	var workers sync.WaitGroup
	for i := range 120 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			key := fmt.Sprintf("session-%d", i)
			first, second := p.CursorFor(key), p.CursorFor(key)
			if first.Next() != second.Next() {
				t.Error("session moved between healthy nodes")
			}
		}()
	}
	workers.Wait()
	counts, moves := p.sessions.snapshot()
	if len(counts) != 3 || moves != 0 {
		t.Fatalf("counts=%v moves=%d", counts, moves)
	}
	for _, count := range counts {
		if count != 40 {
			t.Fatalf("sessions not evenly distributed: %v", counts)
		}
	}
}

func TestStickyRoundRobinTransfersAndRemembersRetry(t *testing.T) {
	p := testAnonymousPool()
	p.strategy = "sticky_round_robin"
	cursor := p.CursorFor("conversation")
	first := cursor.Next()
	p.MarkFailure(first, &http.Response{StatusCode: 429, Header: http.Header{}}, nil)
	replacement := cursor.Next()
	if replacement == first || replacement == nil {
		t.Fatal("failed node did not transfer")
	}
	next := p.CursorFor("conversation")
	if next.Next() != replacement {
		t.Fatal("next turn did not follow retry migration")
	}
	counts, moves := p.sessions.snapshot()
	if moves != 1 || len(counts) != 1 || counts[config.Fingerprint(replacement.proxy.name)] != 1 {
		t.Fatalf("incorrect transfer accounting: %v, %d", counts, moves)
	}
	p.MarkSuccess(first)
	next = p.CursorFor("conversation")
	if next.Next() != replacement {
		t.Fatal("recovery moved a healthy conversation")
	}
}

func TestStickyRoundRobinUnavailableNodeAndSessionExpiry(t *testing.T) {
	p := testAnonymousPool()
	p.strategy = "sticky_round_robin"
	first := p.CursorFor("one")
	node := first.Next()
	node.proxy.healthy.Store(false)
	transferred := p.CursorFor("one")
	if transferred.Next() == node {
		t.Fatal("unhealthy node kept its conversation")
	}
	now := time.Now()
	p.sessions.now = func() time.Time { return now }
	_ = p.CursorFor("two")
	now = now.Add(sessionIdleTTL + time.Second)
	counts, _ := p.sessions.snapshot()
	if len(counts) != 0 {
		t.Fatalf("idle sessions retained: %v", counts)
	}
}

func TestStickyRetryMigrationsStayBalanced(t *testing.T) {
	p := testAnonymousPool()
	p.strategy = "sticky_round_robin"
	var affected []*anonymousCursor
	for i := range 120 {
		cursor := p.CursorFor(fmt.Sprintf("session-%d", i))
		if cursor.Next() == p.nodes[0] {
			affected = append(affected, &cursor)
		}
	}
	p.nodes[0].proxy.healthy.Store(false)
	for _, cursor := range affected {
		if cursor.Next() == p.nodes[0] {
			t.Fatal("retry used failed proxy")
		}
	}
	counts, moves := p.sessions.snapshot()
	if len(counts) != 2 || moves != 40 {
		t.Fatalf("migration counts incorrect: %v / %d", counts, moves)
	}
	for _, count := range counts {
		if count != 60 {
			t.Fatalf("retry transfers not balanced: %v", counts)
		}
	}
}

func TestKeyedStickySessionsBalanceProxiesRatherThanKeyCount(t *testing.T) {
	transports := testAnonymousPool()
	pool := &proxyTransports{strategy: "sticky_round_robin"}
	for _, node := range transports.nodes {
		pool.items = append(pool.items, node.proxy)
	}
	keys, err := nodePool([]string{"a", "b", "c", "d"}, pool, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	for i := range 90 {
		cursor := keys.CursorFor(fmt.Sprintf("key-session-%d", i))
		if cursor.Next() == nil {
			t.Fatal("missing keyed node")
		}
	}
	counts, _ := keys.sessions.snapshot()
	for _, count := range counts {
		if count != 30 {
			t.Fatalf("key count biased proxy distribution: %v", counts)
		}
	}
	if len(counts) != 3 {
		t.Fatalf("not all proxies selected: %v", counts)
	}
}

func TestStickySessionsSurviveRouteReorderAndDropRemovedNodes(t *testing.T) {
	s := newSessionScheduler()
	routes := []sessionRoute{{id: "a", group: "a", ready: true}, {id: "b", group: "b", ready: true}}
	chosen := s.choose("one", routes)
	id := routes[chosen].id
	routes[0], routes[1] = routes[1], routes[0]
	s.reconcile(routes)
	if routes[s.choose("one", routes)].id != id {
		t.Fatal("reorder lost affinity")
	}
	s.reconcile(nil)
	counts, _ := s.snapshot()
	if len(counts) != 0 {
		t.Fatalf("removed nodes retain sessions: %v", counts)
	}
}

func TestStickyRuntimeReloadAndRejectedSavePreserveAssignments(t *testing.T) {
	cfg, err := config.Load("../../config.example.json")
	if err != nil {
		t.Fatal(err)
	}
	cfg.WebUI.Enabled, cfg.WebUI.Password = false, ""
	cfg.Anonymous, cfg.ZenKeys, cfg.GoKeys = true, nil, nil
	cfg.Proxies = []string{"http://127.0.0.1:9000", "http://127.0.0.1:9001"}
	cfg.ProxyStrategy = "sticky_round_robin"
	cfg.Upstream.Zen, cfg.Upstream.Go, cfg.Upstream.Anonymous = "http://127.0.0.1:1", "http://127.0.0.1:1", "http://127.0.0.1:1"
	path := filepath.Join(t.TempDir(), "config.json")
	cfg, err = config.Normalize(path, cfg)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	manager, err := RuntimeManager(ctx, path, cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), telemetry.Monitor(), telemetry.LogHub(100), config.SecretRedactor(), new(slog.LevelVar))
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Shutdown()
	cursor := manager.current.Load().gateway.anonymous.CursorFor("conversation")
	assigned := cursor.Next().proxy.name
	cfg = manager.Config()
	cfg.Proxies[0], cfg.Proxies[1] = cfg.Proxies[1], cfg.Proxies[0]
	if _, err := manager.Apply(cfg, false); err != nil {
		t.Fatal(err)
	}
	cursor = manager.current.Load().gateway.anonymous.CursorFor("conversation")
	if cursor.Next().proxy.name != assigned {
		t.Fatal("hot reload lost session affinity")
	}
	manager.configPath = filepath.Join(filepath.Dir(path), "missing-parent", "config.json")
	cfg.Proxies = []string{"http://127.0.0.1:9002"}
	if _, err := manager.Apply(cfg, true); err == nil {
		t.Fatal("expected persistence failure")
	}
	counts, _ := manager.current.Load().gateway.anonymous.sessions.snapshot()
	if counts[config.Fingerprint(assigned)] != 1 {
		t.Fatalf("rejected save modified live session state: %v", counts)
	}
}
