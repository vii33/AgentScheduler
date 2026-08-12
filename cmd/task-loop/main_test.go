package main

import (
	"slices"
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

	slots := matchingSlots(schedule, mustTime(t, "2026-03-07T22:10:00Z"), mustTime(t, "2026-03-07T22:20:00Z"))
	if len(slots) != 1 {
		t.Fatalf("expected one slot, got %d", len(slots))
	}
	if got := formatTime(slots[0]); got != "2026-03-07T22:15:00Z" {
		t.Fatalf("expected delayed Berlin slot at 23:15, got %s", got)
	}
}

func TestMatchingSlotsUsesEuropeBerlinAcrossDaylightSavingTime(t *testing.T) {
	schedule, err := parseCron("20 8 * * 1-5")
	if err != nil {
		t.Fatal(err)
	}

	winterSlots := matchingSlots(schedule, mustTime(t, "2026-01-12T07:15:00Z"), mustTime(t, "2026-01-12T07:25:00Z"))
	if len(winterSlots) != 1 || formatTime(winterSlots[0]) != "2026-01-12T07:20:00Z" {
		t.Fatalf("expected 08:20 CET slot, got %v", winterSlots)
	}

	summerSlots := matchingSlots(schedule, mustTime(t, "2026-08-10T06:15:00Z"), mustTime(t, "2026-08-10T06:25:00Z"))
	if len(summerSlots) != 1 || formatTime(summerSlots[0]) != "2026-08-10T06:20:00Z" {
		t.Fatalf("expected 08:20 CEST slot, got %v", summerSlots)
	}
}

func TestTaskPlaceholdersUseEuropeBerlinBusinessDate(t *testing.T) {
	got := applyTaskPlaceholders("date=YYYY-MM-DD week=YYYY-Www", mustTime(t, "2026-08-09T22:30:00Z"))
	if want := "date=2026-08-10 week=2026-W33"; got != want {
		t.Fatalf("expected %q, got %q", want, got)
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
	if !schedule.matches(mustTime(t, "2026-03-08T08:00:00Z")) {
		t.Fatal("expected day-of-week 7 to match Sunday")
	}
	if schedule.matches(mustTime(t, "2026-03-09T08:00:00Z")) {
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

func TestValidateTaskAcceptsAgentKinds(t *testing.T) {
	for _, kind := range []string{taskKindOpencode, taskKindCopilotCLI, taskKindClaude, taskKindCodex, taskKindPiAgent} {
		t.Run(kind, func(t *testing.T) {
			err := validateTask(task{
				ID:          "agent-task",
				Enabled:     enabled(true),
				Schedule:    "0 9 * * *",
				Missed:      "run-latest",
				Kind:        kind,
				Instruction: "Summarize this repository.",
			}, 0, map[string]bool{})
			if err != nil {
				t.Fatalf("expected kind %s to validate, got %v", kind, err)
			}
		})
	}
}

func TestValidateTaskRejectsAgentTaskWithoutInstruction(t *testing.T) {
	err := validateTask(task{
		ID:       "missing-instruction",
		Enabled:  enabled(true),
		Schedule: "0 9 * * *",
		Missed:   "run-latest",
		Kind:     taskKindCodex,
	}, 0, map[string]bool{})
	if err == nil {
		t.Fatal("expected missing agent instruction to fail validation")
	}
	if !strings.Contains(err.Error(), "no valid 'instruction' field") {
		t.Fatalf("expected instruction validation error, got %v", err)
	}
}

func TestValidateTaskRejectsThinkingForNonOpencodeTask(t *testing.T) {
	err := validateTask(task{
		ID:          "codex-thinking",
		Enabled:     enabled(true),
		Schedule:    "0 9 * * *",
		Missed:      "run-latest",
		Kind:        taskKindCodex,
		Instruction: "Summarize this repository.",
		Thinking:    "medium",
	}, 0, map[string]bool{})
	if err == nil {
		t.Fatal("expected thinking on non-opencode task to fail validation")
	}
	if !strings.Contains(err.Error(), "only supported for kind=opencode") {
		t.Fatalf("expected thinking validation error, got %v", err)
	}
}

func TestOpencodeArgsIncludeThinkingVariant(t *testing.T) {
	want := []string{"run", "-m", "github-copilot/gpt-5.5", "--variant", "medium", "explain"}
	if got := opencodeArgs("explain", "github-copilot/gpt-5.5", " medium "); !slices.Equal(got, want) {
		t.Fatalf("expected args %#v, got %#v", want, got)
	}

	withoutThinking := []string{"run", "-m", "github-copilot/gpt-5.5", "explain"}
	if got := opencodeArgs("explain", "github-copilot/gpt-5.5", ""); !slices.Equal(got, withoutThinking) {
		t.Fatalf("expected args %#v, got %#v", withoutThinking, got)
	}
}

func TestAgentAdapterArgs(t *testing.T) {
	tests := []struct {
		kind        string
		instruction string
		model       string
		want        []string
	}{
		{taskKindCopilotCLI, "explain", "gpt-5.3-codex", []string{"-p", "explain", "-s", "--model", "gpt-5.3-codex"}},
		{taskKindClaude, "explain", "sonnet", []string{"-p", "--model", "sonnet", "explain"}},
		{taskKindCodex, "explain", "gpt-5.3-codex", []string{"exec", "--model", "gpt-5.3-codex", "explain"}},
		{taskKindPiAgent, "explain", "openai/gpt-4o", []string{"--model", "openai/gpt-4o", "-p", "explain"}},
	}
	for _, tt := range tests {
		t.Run(tt.kind, func(t *testing.T) {
			adapter := agentAdapters[tt.kind]
			if got := adapter.args(tt.instruction, tt.model); !slices.Equal(got, tt.want) {
				t.Fatalf("expected args %#v, got %#v", tt.want, got)
			}
		})
	}
}

func TestTaskModelPrefersTaskModelThenAgentEnv(t *testing.T) {
	t.Setenv("CODEX_TASK_MODEL", "from-env")
	adapter := agentAdapters[taskKindCodex]
	if got := taskModel(task{Model: "from-task"}, adapter); got != "from-task" {
		t.Fatalf("expected task model to win, got %q", got)
	}
	if got := taskModel(task{}, adapter); got != "from-env" {
		t.Fatalf("expected model from env, got %q", got)
	}
}

func TestShellAllowlistDoesNotAllowAgentBinaries(t *testing.T) {
	for _, command := range []string{"copilot -p hi", "claude -p hi", "codex exec hi", "pi -p hi"} {
		if shellCommandAllowed(command) {
			t.Fatalf("expected shell allowlist to reject %q", command)
		}
	}
}

func TestShellAllowlistAcceptsSiblingRepositoryScripts(t *testing.T) {
	for _, command := range []string{
		"../agentic-memories/scripts/export-sessions.sh",
		"../teams-daily-bot/scripts/reconcile-daily-attendees.sh YYYY-MM-DD",
	} {
		if !shellCommandAllowed(command) {
			t.Fatalf("expected shell allowlist to accept %q", command)
		}
	}
}

func TestShellAllowlistRejectsUnapprovedSiblingPaths(t *testing.T) {
	for _, command := range []string{
		"../agentic-memories/MEMORY.md",
		"../../other-repository/scripts/run.sh",
		"../teams-daily-bot/config/live.json",
	} {
		if shellCommandAllowed(command) {
			t.Fatalf("expected shell allowlist to reject %q", command)
		}
	}
}
