package admin

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"opencode2api/internal/config"
	"opencode2api/internal/gateway"
	"opencode2api/internal/telemetry"
)

func proxyTestServer(t *testing.T) (*Server, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	cfg, err := config.Load("../../config.example.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "extra.txt"), []byte("http://127.0.0.1:8000\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg.WebUI.Password, cfg.WebUI.PasswordHash = "", "test-hash"
	cfg.Anonymous, cfg.ZenKeys, cfg.GoKeys = true, nil, nil
	cfg.ProxyFiles, cfg.ProxyStrategy = []string{"extra.txt"}, "round_robin"
	cfg.Upstream.Zen, cfg.Upstream.Go, cfg.Upstream.Anonymous = "http://127.0.0.1:1", "http://127.0.0.1:1", "http://127.0.0.1:1"
	cfg, err = config.Normalize(path, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := config.SaveAtomic(path, cfg); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Tests must not start background network probes.
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	monitor := telemetry.NewMonitor()
	hub := telemetry.NewLogHub(100)
	manager, err := gateway.NewRuntimeManager(ctx, path, cfg, logger, monitor, hub, config.NewSecretRedactor(), new(slog.LevelVar))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.Shutdown)
	a := New(manager, monitor, hub, logger)
	a.sessions[tokenDigest("test-session")] = adminSession{Username: cfg.WebUI.Username, AuthVersion: config.Fingerprint(cfg.WebUI.PasswordHash), CSRF: "test-csrf", Expires: time.Now().Add(time.Hour)}
	return a, path
}

func TestProxyImportAuthenticationAndAtomicParsing(t *testing.T) {
	a, _ := proxyTestServer(t)
	body := `{"files":[{"name":"one.txt","content":"http://127.0.0.1:9000\n"},{"name":"two.csv","content":"URL\nhttp://127.0.0.1:9000\nhttp://127.0.0.1:9001\n"}]}`
	for _, test := range []struct {
		cookie, csrf string
		status       int
	}{{"", "", 401}, {"test-session", "", 403}, {"test-session", "test-csrf", 200}} {
		r := httptest.NewRequest("POST", "/api/proxies/parse", strings.NewReader(body))
		if test.cookie != "" {
			r.AddCookie(&http.Cookie{Name: adminCookieName, Value: test.cookie})
		}
		r.Header.Set("X-CSRF-Token", test.csrf)
		w := httptest.NewRecorder()
		a.Handler().ServeHTTP(w, r)
		if w.Code != test.status {
			t.Fatalf("status=%d want=%d: %s", w.Code, test.status, w.Body.String())
		}
		if w.Code == 200 {
			var result struct {
				Count int `json:"count"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil || result.Count != 2 || w.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("incorrect import result: %s", w.Body.String())
			}
		}
	}
	if got := a.manager.Config(); len(got.Proxies) != 1 || len(got.ProxyFiles) != 1 {
		t.Fatal("parse endpoint mutated live configuration")
	}
}

func TestLegacyConfigUpdatePreservesNewProxySettings(t *testing.T) {
	a, path := proxyTestServer(t)
	cfg := a.manager.Config()
	update := ConfigUpdate{
		Listen: cfg.Listen, Anonymous: cfg.Anonymous, Prefer: cfg.Prefer,
		ServerKeys: []SecretInput{{ID: config.Fingerprint(cfg.ServerKeys[0])}},
		Proxies:    []SecretInput{{Value: "direct"}},
		Upstream:   cfg.Upstream, Retry: cfg.Retry, Models: cfg.Models,
		Performance: cfg.Performance, Logging: cfg.Logging,
		WebUI: WebUIView{Enabled: cfg.WebUI.Enabled, Listen: cfg.WebUI.Listen, SessionTTLMinutes: cfg.WebUI.SessionTTLMinutes},
	}
	data, _ := json.Marshal(update)
	w := httptest.NewRecorder()
	a.handlePutConfig(w, httptest.NewRequest("PUT", "/api/config", strings.NewReader(string(data))))
	if w.Code != 200 {
		t.Fatalf("save failed: %s", w.Body.String())
	}
	saved, err := config.Load(path)
	if err != nil || saved.ProxyStrategy != "round_robin" || len(saved.ProxyFiles) != 1 || len(saved.Proxies) != 1 {
		t.Fatalf("legacy save lost new settings: strategy=%s files=%v error=%v", saved.ProxyStrategy, saved.ProxyFiles, err)
	}
	strategy := "least_connections"
	update.ProxyStrategy = &strategy
	files := []string{}
	update.ProxyFiles = &files
	data, _ = json.Marshal(update)
	w = httptest.NewRecorder()
	a.handlePutConfig(w, httptest.NewRequest("PUT", "/api/config", strings.NewReader(string(data))))
	saved, err = config.Load(path)
	if w.Code != 200 || err != nil || saved.ProxyStrategy != strategy || len(saved.ProxyFiles) != 0 {
		t.Fatalf("explicit update was not persisted: %s, %v", w.Body.String(), err)
	}
}
