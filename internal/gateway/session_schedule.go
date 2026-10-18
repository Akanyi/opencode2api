package gateway

import (
	"container/list"
	"sync"
	"time"

	"opencode2api/internal/config"
)

const sessionIdleTTL = 30 * time.Minute
const maximumStickySessions = 20000

type sessionRoute struct {
	id, group string
	ready     bool
}

type sessionBinding struct {
	session, route, group string
	lastSeen              time.Time
}

// A shared scheduler reserves a destination before dialing, so concurrent new
// sessions distribute evenly even while all connection counts are still zero.
type sessionScheduler struct {
	mu         sync.Mutex
	bindings   map[string]*list.Element
	order      list.List
	counts     map[string]int
	next       uint64
	migrations uint64
	now        func() time.Time
}

func newSessionScheduler() *sessionScheduler {
	return &sessionScheduler{bindings: make(map[string]*list.Element), counts: make(map[string]int), now: time.Now}
}

func (s *sessionScheduler) removeLocked(element *list.Element) {
	binding := element.Value.(*sessionBinding)
	s.counts[binding.group]--
	if s.counts[binding.group] == 0 {
		delete(s.counts, binding.group)
	}
	delete(s.bindings, binding.session)
	s.order.Remove(element)
}

func (s *sessionScheduler) expireLocked(now time.Time) {
	for element := s.order.Front(); element != nil; element = s.order.Front() {
		if now.Sub(element.Value.(*sessionBinding).lastSeen) < sessionIdleTTL {
			break
		}
		s.removeLocked(element)
	}
}

func (s *sessionScheduler) bindLocked(session string, route sessionRoute, now time.Time) {
	if element := s.bindings[session]; element != nil {
		binding := element.Value.(*sessionBinding)
		if binding.route != route.id || binding.group != route.group {
			s.counts[binding.group]--
			if s.counts[binding.group] == 0 {
				delete(s.counts, binding.group)
			}
			s.counts[route.group]++
			s.migrations++
		}
		binding.route, binding.group, binding.lastSeen = route.id, route.group, now
		s.order.MoveToBack(element)
		return
	}
	for len(s.bindings) >= maximumStickySessions {
		s.removeLocked(s.order.Front())
	}
	binding := &sessionBinding{session: session, route: route.id, group: route.group, lastSeen: now}
	s.bindings[session] = s.order.PushBack(binding)
	s.counts[route.group]++
}

func (s *sessionScheduler) remember(session string, route sessionRoute) {
	if session == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	s.expireLocked(now)
	s.bindLocked(session, route, now)
}

func (s *sessionScheduler) choose(session string, routes []sessionRoute) int {
	if len(routes) == 0 {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	s.expireLocked(now)
	if element := s.bindings[session]; session != "" && element != nil {
		binding := element.Value.(*sessionBinding)
		for i, route := range routes {
			if route.id == binding.route && route.ready {
				s.bindLocked(session, route, now)
				return i
			}
		}
	}
	start := int(s.next % uint64(len(routes)))
	s.next++
	choice, minimum := -1, 0
	for offset := range routes {
		i := (start + offset) % len(routes)
		if !routes[i].ready {
			continue
		}
		load := s.counts[routes[i].group]
		if choice < 0 || load < minimum {
			choice, minimum = i, load
		}
	}
	if choice < 0 {
		return start
	} // Existing retry fallback handles total outages.
	if session != "" {
		s.bindLocked(session, routes[choice], now)
	}
	return choice
}

func (s *sessionScheduler) reconcile(routes []sessionRoute) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.expireLocked(s.now())
	known := make(map[string]sessionRoute, len(routes))
	for _, route := range routes {
		known[route.id] = route
	}
	for element := s.order.Front(); element != nil; {
		next := element.Next()
		binding := element.Value.(*sessionBinding)
		if route, ok := known[binding.route]; !ok {
			s.removeLocked(element)
		} else if binding.group != route.group {
			s.counts[binding.group]--
			if s.counts[binding.group] == 0 {
				delete(s.counts, binding.group)
			}
			binding.group = route.group
			s.counts[route.group]++
			s.migrations++
		}
		element = next
	}
}

func (s *sessionScheduler) snapshot() (map[string]int, uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.expireLocked(s.now())
	counts := make(map[string]int, len(s.counts))
	for group, count := range s.counts {
		counts[group] = count
	}
	return counts, s.migrations
}

func anonymousSessionRoute(node *anonymousNode) sessionRoute {
	id := config.Fingerprint(node.proxy.name)
	return sessionRoute{id: id, group: id, ready: node.proxy.pool || (node.proxy.healthy.Load() && node.cooldownUntil.Load() <= time.Now().UnixNano())}
}

func (p *anonymousPool) sessionRoutes() []sessionRoute {
	routes := make([]sessionRoute, len(p.nodes))
	for i, node := range p.nodes {
		routes[i] = anonymousSessionRoute(node)
	}
	return routes
}

func (p *nodePool) sessionRoute(node *upstreamNode) sessionRoute {
	proxy := p.Proxy(node)
	route := sessionRoute{id: config.Fingerprint(node.key)}
	if proxy != nil {
		route.group = config.Fingerprint(proxy.name)
		route.ready = proxy.healthy.Load() && node.cooldownUntil.Load() <= time.Now().UnixNano()
	}
	return route
}

func (p *nodePool) sessionRoutes() []sessionRoute {
	routes := make([]sessionRoute, len(p.nodes))
	for i, node := range p.nodes {
		routes[i] = p.sessionRoute(node)
	}
	return routes
}
