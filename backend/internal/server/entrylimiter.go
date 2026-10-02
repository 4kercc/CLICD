package server

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Entry gate rate limiting: an IP whose unauthenticated requests get rejected
// by the gate too often is banned outright, which turns prefix enumeration
// from "hours at thousands of requests per second" into "30 guesses per hour".
const (
	entryRateWindow1   = time.Minute
	entryRateLimit1    = 30
	entryRateWindow2   = 10 * time.Minute
	entryRateLimit2    = 100
	entryBanDuration   = time.Hour
	entryLimiterMaxIPs = 65536
)

type entryIPState struct {
	stamps      []time.Time // gate-rejected requests inside the wide window
	bannedUntil time.Time
	lastSeen    time.Time
}

type entryLimiter struct {
	mu  sync.Mutex
	ips map[string]*entryIPState
	now func() time.Time
}

func newEntryLimiter() *entryLimiter {
	return &entryLimiter{
		ips: make(map[string]*entryIPState),
		now: time.Now,
	}
}

// hit records a gate-rejected request for an IP and reports whether the IP is
// banned — including a ban this very request has just triggered. Reaching
// either the per-minute or the ten-minute limit bans the IP outright.
func (l *entryLimiter) hit(ip string) (banned, justBanned bool) {
	now := l.now()
	l.mu.Lock()
	defer l.mu.Unlock()

	state, ok := l.ips[ip]
	if !ok {
		if len(l.ips) >= entryLimiterMaxIPs {
			l.sweepLocked(now)
		}
		state = &entryIPState{}
		l.ips[ip] = state
	}
	state.lastSeen = now
	if state.bannedUntil.After(now) {
		return true, false
	}

	wide := now.Add(-entryRateWindow2)
	narrow := now.Add(-entryRateWindow1)
	kept := state.stamps[:0]
	for _, stamp := range state.stamps {
		if stamp.After(wide) {
			kept = append(kept, stamp)
		}
	}
	stamps := append(kept, now)
	state.stamps = stamps

	if len(stamps) >= entryRateLimit2 {
		state.bannedUntil = now.Add(entryBanDuration)
		state.stamps = state.stamps[:0]
		return true, true
	}
	narrowCount := 0
	for _, stamp := range stamps {
		if stamp.After(narrow) {
			narrowCount++
		}
	}
	if narrowCount >= entryRateLimit1 {
		state.bannedUntil = now.Add(entryBanDuration)
		state.stamps = state.stamps[:0]
		return true, true
	}
	return false, false
}

func (l *entryLimiter) isBanned(ip string) bool {
	now := l.now()
	l.mu.Lock()
	defer l.mu.Unlock()
	state, ok := l.ips[ip]
	return ok && state.bannedUntil.After(now)
}

func (l *entryLimiter) sweepLocked(now time.Time) {
	cutoff := now.Add(-2 * entryBanDuration)
	for ip, state := range l.ips {
		if state.lastSeen.Before(cutoff) {
			delete(l.ips, ip)
		}
	}
}

// startJanitor drops IPs that have been inactive for longer than a ban could
// last, keeping the map bounded under distributed scanning.
func (l *entryLimiter) startJanitor() {
	go func() {
		ticker := time.NewTicker(10 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			l.mu.Lock()
			l.sweepLocked(l.now())
			l.mu.Unlock()
		}
	}()
}

// entryClientIP is the peer address of the request. Deliberately NOT
// X-Forwarded-For: that header is attacker-controlled and would let a scanner
// spread its guesses across unlimited fake identities.
func entryClientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
	if err != nil {
		return strings.TrimSpace(r.RemoteAddr)
	}
	return host
}
