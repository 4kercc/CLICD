package api

import (
	"sync"
	"time"
)

// Simple fixed-window brute-force protection for the login endpoints
// (admin login, sub-user login, sub-user access code). Attempts are tracked
// per client IP + username; after too many failures inside the window the
// key is blocked for a cooldown period. Successful logins clear the record.

const (
	loginFailWindow    = time.Minute
	loginMaxFailures   = 5
	loginBlockDuration = 5 * time.Minute
)

type loginAttempt struct {
	failures     int
	windowStart  time.Time
	blockedUntil time.Time
}

var (
	loginRateMu     sync.Mutex
	loginRateLimits = make(map[string]*loginAttempt)
	rateLimitOnce   sync.Once
)

func loginRateAllowed(key string) bool {
	rateLimitOnce.Do(startLoginRateCleanup)
	loginRateMu.Lock()
	defer loginRateMu.Unlock()
	now := time.Now()
	a, ok := loginRateLimits[key]
	if !ok {
		return true
	}
	if a.blockedUntil.After(now) {
		return false
	}
	if now.Sub(a.windowStart) > loginFailWindow && a.blockedUntil.IsZero() {
		a.failures = 0
		a.windowStart = now
	}
	return true
}

func loginRateBlockedSeconds(key string) int {
	loginRateMu.Lock()
	defer loginRateMu.Unlock()
	if a, ok := loginRateLimits[key]; ok {
		if remaining := int(time.Until(a.blockedUntil).Seconds()) + 1; remaining > 0 {
			return remaining
		}
	}
	return int(loginBlockDuration / time.Second)
}

func loginRateRecord(key string, success bool) {
	loginRateMu.Lock()
	defer loginRateMu.Unlock()
	now := time.Now()
	if success {
		delete(loginRateLimits, key)
		return
	}
	a, ok := loginRateLimits[key]
	if !ok || now.Sub(a.windowStart) > loginFailWindow {
		a = &loginAttempt{windowStart: now}
		loginRateLimits[key] = a
	}
	a.failures++
	if a.failures >= loginMaxFailures {
		a.blockedUntil = now.Add(loginBlockDuration)
		a.failures = 0
		a.windowStart = now
	}
}

// startLoginRateCleanup periodically drops stale attempt records so the map
// cannot grow without bound on internet-facing panels.
func startLoginRateCleanup() {
	go func() {
		ticker := time.NewTicker(10 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			loginRateMu.Lock()
			now := time.Now()
			for key, a := range loginRateLimits {
				if now.Sub(a.windowStart) > loginFailWindow && !a.blockedUntil.After(now) {
					delete(loginRateLimits, key)
				}
			}
			loginRateMu.Unlock()
		}
	}()
}
