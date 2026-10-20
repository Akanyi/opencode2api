package gateway

import (
	"hash/fnv"
	"io"
	rand "math/rand/v2"
	"net/http"
	"sync"
	"sync/atomic"
)

// The configured strategy chooses the first route. Existing cooldown and retry
// traversal still decide which routes are usable and provide failover.
func scheduleStart(strategy, affinity string, count int, next *atomic.Uint64, route func(int) (*proxyTransport, bool)) int {
	if count == 0 {
		return 0
	}
	if (strategy == "" || strategy == "affinity") && affinity != "" {
		hash := fnv.New64a()
		_, _ = hash.Write([]byte(affinity))
		return int(hash.Sum64() % uint64(count))
	}
	if strategy == "random" {
		return rand.IntN(count)
	}
	start := int((next.Add(1) - 1) % uint64(count))
	if strategy != "least_connections" {
		return start
	}
	choice := -1
	var minimum int64
	for offset := range count {
		i := (start + offset) % count
		proxy, ready := route(i)
		if proxy == nil || !ready || !proxy.healthy.Load() {
			continue
		}
		active := proxy.active.Load()
		if choice < 0 || active < minimum {
			choice, minimum = i, active
		}
	}
	if choice >= 0 {
		return choice
	}
	return start
}

// Keep a connection active through response consumption, including SSE bodies.
type countingTransport struct {
	base   http.RoundTripper
	active *atomic.Int64
}

func (t *countingTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	t.active.Add(1)
	resp, err := t.base.RoundTrip(r)
	if err != nil {
		t.active.Add(-1)
		return resp, err
	}
	if resp.Body == nil || resp.Body == http.NoBody {
		t.active.Add(-1)
	} else {
		resp.Body = &countingBody{ReadCloser: resp.Body, release: func() { t.active.Add(-1) }}
	}
	return resp, nil
}

func (t *countingTransport) CloseIdleConnections() {
	if closer, ok := t.base.(interface{ CloseIdleConnections() }); ok {
		closer.CloseIdleConnections()
	}
}

func reuseProxyConnectionCounters(previous, next *proxyTransports) {
	known := make(map[string]*proxyTransport)
	for _, proxy := range previous.items {
		known[proxy.name] = proxy
	}
	for _, proxy := range next.items {
		if old := known[proxy.name]; old != nil {
			// Old response bodies remain active while a new runtime serves requests.
			proxy.active = old.active
			proxy.client.Transport.(*countingTransport).active = old.active
			proxy.bun.Transport.(*countingTransport).active = old.active
		}
	}
}

type countingBody struct {
	io.ReadCloser
	once    sync.Once
	release func()
}

func (b *countingBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	if err != nil {
		b.once.Do(b.release)
	}
	return n, err
}

func (b *countingBody) Close() error {
	err := b.ReadCloser.Close()
	b.once.Do(b.release)
	return err
}
