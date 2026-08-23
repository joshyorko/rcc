package environmentlifecycle

import (
	"errors"
	"testing"
	"time"
)

func TestCrashMatrixExecutesEveryLifecyclePoint(t *testing.T) {
	defer SetCrashHook(nil)
	seen := map[CrashPoint]bool{}
	SetCrashHook(func(point CrashPoint) error { seen[point] = true; return errors.New("injected crash") })
	for _, point := range CrashPoints() { _ = crash(point) }
	for _, point := range CrashPoints() { if !seen[point] { t.Fatalf("crash point %q was not executed", point) } }
}

func TestRetentionEligibilityUsesInjectedClock(t *testing.T) {
	now := time.Unix(100, 0)
	record := materializationRecord{VerifiedAt: now.Add(-10 * time.Second)}
	if RetentionEligible(record, now, 11*time.Second) { t.Fatal("record incorrectly eligible") }
	if !RetentionEligible(record, now, 10*time.Second) { t.Fatal("record at retention boundary not eligible") }
}
