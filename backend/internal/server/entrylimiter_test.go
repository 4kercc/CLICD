package server

import (
	"testing"
	"time"
)

// The limiter must run on an injected clock so the windows and the ban expiry
// can be exercised without sleeping.
func newTestLimiter() (*entryLimiter, *time.Time) {
	now := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	l := newEntryLimiter()
	l.now = func() time.Time { return now }
	return l, &now
}

func advance(t *testing.T, now *time.Time, d time.Duration) {
	t.Helper()
	*now = now.Add(d)
}

func TestEntryLimiterBansAfterPerMinuteLimit(t *testing.T) {
	l, _ := newTestLimiter()
	for i := 1; i <= entryRateLimit1-1; i++ {
		if banned, justBanned := l.hit("1.2.3.4"); banned {
			t.Fatalf("hit %d banned early (banned=%v justBanned=%v)", i, banned, justBanned)
		}
	}
	banned, justBanned := l.hit("1.2.3.4")
	if !banned || !justBanned {
		t.Fatalf("hit %d should trigger the ban (banned=%v justBanned=%v)", entryRateLimit1, banned, justBanned)
	}
	if !l.isBanned("1.2.3.4") {
		t.Fatal("IP should be banned after the per-minute limit")
	}
	// Every further request stays banned but must not re-trigger the audit path.
	if banned, justBanned := l.hit("1.2.3.4"); !banned || justBanned {
		t.Fatalf("banned IP: banned=%v justBanned=%v, want banned=true justBanned=false", banned, justBanned)
	}
}

func TestEntryLimiterBansAfterTenMinuteLimit(t *testing.T) {
	l, now := newTestLimiter()
	// Stay under the per-minute limit (one guess every 5s) but keep all 100
	// requests inside the wide window (100 × 5s = 500s < 600s).
	for i := 1; i <= entryRateLimit2-1; i++ {
		if banned, _ := l.hit("1.2.3.4"); banned {
			t.Fatalf("hit %d banned early", i)
		}
		advance(t, now, 5*time.Second)
	}
	if banned, justBanned := l.hit("1.2.3.4"); !banned || !justBanned {
		t.Fatalf("hit %d should ban via the ten-minute window (banned=%v justBanned=%v)",
			entryRateLimit2, banned, justBanned)
	}
}

func TestEntryLimiterBanExpiresAndRestartsFresh(t *testing.T) {
	l, now := newTestLimiter()
	for i := 0; i < entryRateLimit1; i++ {
		l.hit("1.2.3.4")
	}
	if !l.isBanned("1.2.3.4") {
		t.Fatal("expected a ban")
	}
	advance(t, now, entryBanDuration+time.Second)
	if l.isBanned("1.2.3.4") {
		t.Fatal("ban must expire after one hour")
	}
	// The counters were cleared when the ban started: the IP gets a fresh batch.
	for i := 1; i < entryRateLimit1; i++ {
		if banned, _ := l.hit("1.2.3.4"); banned {
			t.Fatalf("fresh batch banned early at hit %d", i)
		}
	}
	if banned, justBanned := l.hit("1.2.3.4"); !banned || !justBanned {
		t.Fatal("the fresh batch must reach the limit again")
	}
}

func TestEntryLimiterIsolatesIPs(t *testing.T) {
	l, _ := newTestLimiter()
	for i := 0; i < entryRateLimit1; i++ {
		l.hit("1.2.3.4")
	}
	if !l.isBanned("1.2.3.4") {
		t.Fatal("attacker IP should be banned")
	}
	if l.isBanned("5.6.7.8") {
		t.Fatal("a different IP must not be banned")
	}
	if banned, _ := l.hit("5.6.7.8"); banned {
		t.Fatal("a different IP must keep its own counters")
	}
}

func TestEntryLimiterOldRequestsFallOutOfTheWindows(t *testing.T) {
	l, now := newTestLimiter()
	// Fill almost the whole narrow limit, then let the minute pass.
	for i := 0; i < entryRateLimit1-1; i++ {
		l.hit("1.2.3.4")
	}
	advance(t, now, 2*entryRateWindow1)
	for i := 0; i < entryRateLimit1-1; i++ {
		if banned, _ := l.hit("1.2.3.4"); banned {
			t.Fatalf("stale requests must not count towards the fresh minute (hit %d banned)", i)
		}
	}
}
