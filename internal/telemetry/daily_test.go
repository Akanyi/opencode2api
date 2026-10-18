package telemetry

import (
	"testing"
	"time"

	"opencode2api/internal/protocol"
)

func TestTodayIncludesEarlierHoursAndExcludesPreviousDay(t *testing.T) {
	m := NewMonitor()
	now := time.Date(2026, 10, 17, 23, 59, 0, 0, statisticsLocation)
	m.now = func() time.Time { return now }
	meta := &RequestMeta{Request: "r", Model: "m", Tier: "zen", ServerKeyID: "key", UsageReported: true, Usage: protocol.Usage{Input: 10, Output: 20, Total: 30}}
	m.Record("/v1/chat/completions", 200, time.Second, meta)
	m.RecordAttempt(UpstreamAttempt{ProxyID: "yesterday", Proxy: "same-redacted-address", Success: true})
	now = now.Add(2 * time.Minute)
	m.Record("/v1/chat/completions", 200, time.Second, meta)
	m.RecordAttempt(UpstreamAttempt{ProxyID: "first", Proxy: "same-redacted-address", Success: true})
	now = now.Add(9 * time.Hour)
	m.Record("/v1/chat/completions", 500, 2*time.Second, meta)
	m.RecordAttempt(UpstreamAttempt{ProxyID: "second", Proxy: "same-redacted-address", Success: false})
	s := m.Snapshot()
	if s.Today.Total != 2 || s.Window.Total != 1 || s.Today.SuccessRate != .5 || s.Usage.Today.Tokens.Total != 60 {
		t.Fatalf("today=%+v hour=%+v usage=%+v", s.Today, s.Window, s.Usage.Today)
	}
	if len(s.Upstream.Today.Proxies) != 2 || s.Upstream.Today.Proxies["first"].Success != 1 || s.Upstream.Today.Proxies["second"].Failed != 1 {
		t.Fatalf("proxy buckets mixed: %+v", s.Upstream.Today.Proxies)
	}
	if s.Endpoints["/v1/chat/completions"] != 2 || len(s.TodaySeries) != 542 || m.TodayServerKeyUsage()["key"].Requests != 2 {
		t.Fatalf("daily breakdown or series incorrect: %d points", len(s.TodaySeries))
	}
	if s.DayStart.Hour() != 0 || s.DayStart.UTC().Hour() != 16 || s.Timezone != "Asia/Singapore" {
		t.Fatalf("incorrect calendar boundary: %v", s.DayStart)
	}
	now = time.Date(2026, 10, 19, 0, 0, 0, 0, statisticsLocation)
	if next := m.Snapshot(); next.Today.Total != 0 || next.Usage.Today.Tokens.Total != 0 || next.Upstream.Today.Total != 0 {
		t.Fatalf("previous day's data survived midnight: %+v", next.Today)
	}
}

func TestDailyAggregationDoesNotDependOnRecentRing(t *testing.T) {
	m := NewMonitor()
	for range monitorRecentRequestCapacity + 1 {
		m.Record("/v1/chat/completions", 200, time.Millisecond, &RequestMeta{Request: "r", Model: "m"})
	}
	s := m.Snapshot()
	if s.Today.Total != monitorRecentRequestCapacity+1 || len(s.Upstream.Requests) != monitorRecentOutputLimit {
		t.Fatalf("aggregate=%d recent=%d", s.Today.Total, len(s.Upstream.Requests))
	}
}

func TestConcurrentPeakAcrossMidnight(t *testing.T) {
	m := NewMonitor()
	now := time.Date(2026, 10, 18, 23, 59, 0, 0, statisticsLocation)
	m.now = func() time.Time { return now }
	m.BeginRequest()
	m.BeginRequest()
	m.BeginStream()
	s := m.Snapshot()
	if s.Active != 2 || s.ActiveStreams != 1 || s.Today.PeakConcurrency != 2 {
		t.Fatalf("incorrect concurrency: %+v", s)
	}
	now = now.Add(2 * time.Minute)
	m.EndStream()
	m.EndRequest()
	m.EndRequest()
	s = m.Snapshot()
	if s.Active != 0 || s.ActiveStreams != 0 || s.Today.PeakConcurrency != 2 {
		t.Fatalf("lost overnight peak: %+v", s.Today)
	}
}
