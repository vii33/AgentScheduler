package main

import (
	"strings"
	"testing"
	"time"
)

func mustTime(t *testing.T, value string) time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}

func enabled(value bool) *bool {
	return &value
}

func TestMatchingSlotsFindsDelayedCronSlot(t *testing.T) {
	schedule, err := parseCron("15 23 * * *")
	if err != nil {
		t.Fatal(err)
	}

	slots := matchingSlots(schedule, mustTime(t, "2026-03-07T23:10:00Z"), mustTime(t, "2026-03-07T23:20:00Z"))
	if len(slots) != 1 {
		t.Fatalf("expected one slot, got %d", len(slots))
	}
	if got := formatTime(slots[0]); got != "2026-03-07T23:15:00Z" {
		t.Fatalf("expected delayed slot at 23:15, got %s", got)
	}
}

func TestRunLatestKeepsOnlyNewestMissedSlot(t *testing.T) {
	tasks := []task{{
		ID:       "frequent",
		Enabled:  enabled(true),
		Schedule: "0 * * * *",
		Missed:   "run-latest",
		Kind:     "shell",
		Command:  "./scripts/example.sh",
	}}

	runs, err := findDueRuns(tasks, mustTime(t, "2026-03-07T09:00:00Z"), mustTime(t, "2026-03-07T12:30:00Z"), 5*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 1 {
		t.Fatalf("expected one run, got %d", len(runs))
	}
	if got := formatTime(runs[0].scheduledFor); got != "2026-03-07T12:00:00Z" {
		t.Fatalf("expected latest missed slot at noon, got %s", got)
	}
}

func TestSkipIgnoresStaleMissedSlots(t *testing.T) {
	tasks := []task{{
		ID:       "daily",
		Enabled:  enabled(true),
		Schedule: "15 23 * * *",
		Missed:   "skip",
		Kind:     "shell",
		Command:  "./scripts/example.sh",
	}}

	runs, err := findDueRuns(tasks, mustTime(t, "2026-03-06T00:00:00Z"), mustTime(t, "2026-03-07T08:00:00Z"), 5*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 0 {
		t.Fatalf("expected stale missed slot to be skipped, got %d", len(runs))
	}
}

func TestCatchUpKeepsEveryMissedSlot(t *testing.T) {
	tasks := []task{{
		ID:       "hourly",
		Enabled:  enabled(true),
		Schedule: "0 * * * *",
		Missed:   "catch-up",
		Kind:     "shell",
		Command:  "./scripts/example.sh",
	}}

	runs, err := findDueRuns(tasks, mustTime(t, "2026-03-07T09:30:00Z"), mustTime(t, "2026-03-07T12:30:00Z"), 5*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 3 {
		t.Fatalf("expected three catch-up runs, got %d", len(runs))
	}
	want := []string{"2026-03-07T10:00:00Z", "2026-03-07T11:00:00Z", "2026-03-07T12:00:00Z"}
	for i, expected := range want {
		if got := formatTime(runs[i].scheduledFor); got != expected {
			t.Fatalf("run %d: expected %s, got %s", i, expected, got)
		}
	}
}

func TestParseCronTreatsSevenAsSunday(t *testing.T) {
	schedule, err := parseCron("0 9 * * 7")
	if err != nil {
		t.Fatal(err)
	}
	if !schedule.matches(mustTime(t, "2026-03-08T09:00:00Z")) {
		t.Fatal("expected day-of-week 7 to match Sunday")
	}
	if schedule.matches(mustTime(t, "2026-03-09T09:00:00Z")) {
		t.Fatal("did not expect day-of-week 7 to match Monday")
	}
}

func TestValidateTaskRejectsInvalidMissedPolicy(t *testing.T) {
	err := validateTask(task{
		ID:       "bad-missed",
		Enabled:  enabled(true),
		Schedule: "0 9 * * *",
		Missed:   "retry-forever",
		Kind:     "shell",
		Command:  "./scripts/example.sh",
	}, 0, map[string]bool{})
	if err == nil {
		t.Fatal("expected invalid missed policy to fail validation")
	}
	if !strings.Contains(err.Error(), "invalid missed policy") {
		t.Fatalf("expected missed policy error, got %v", err)
	}
}
