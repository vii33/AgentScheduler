package main

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

type recordedRun struct {
	taskID       string
	scheduledFor string
	status       string
	errorText    sql.NullString
}

func testRepo(t *testing.T, tasksYAML string, scripts map[string]string) repoPaths {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "crons"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "scripts"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "crons", "tasks.yaml"), []byte(tasksYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	for name, content := range scripts {
		path := filepath.Join(root, "scripts", name)
		if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return repoPaths{
		repoRoot:  root,
		tasksFile: filepath.Join(root, "crons", "tasks.yaml"),
		dbFile:    filepath.Join(root, "agentscheduler.db"),
		lockFile:  filepath.Join(root, "task-loop.lock"),
	}
}

func readRuns(t *testing.T, paths repoPaths) []recordedRun {
	t.Helper()
	db, err := sql.Open("sqlite", paths.dbFile)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	rows, err := db.Query(`SELECT task_id, scheduled_for, status, error FROM task_runs ORDER BY scheduled_for, id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()

	runs := []recordedRun{}
	for rows.Next() {
		run := recordedRun{}
		if err := rows.Scan(&run.taskID, &run.scheduledFor, &run.status, &run.errorText); err != nil {
			t.Fatal(err)
		}
		runs = append(runs, run)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return runs
}

func readStateValue(t *testing.T, paths repoPaths, key string) string {
	t.Helper()
	db, err := sql.Open("sqlite", paths.dbFile)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var value string
	if err := db.QueryRow(`SELECT value FROM scheduler_state WHERE key = ?`, key).Scan(&value); err != nil {
		t.Fatal(err)
	}
	return value
}

func writeLastCheckedForTest(t *testing.T, paths repoPaths, value string) {
	t.Helper()
	db, err := openDB(paths)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := writeLastChecked(db, mustTime(t, value)); err != nil {
		t.Fatal(err)
	}
}

func TestRunIterationRecordsSuccessfulShellTask(t *testing.T) {
	paths := testRepo(t, `tasks:
  - id: record-success
    enabled: true
    schedule: "15 23 * * *"
    missed: run-latest
    kind: shell
    command: "./scripts/record.sh"
`, map[string]string{
		"record.sh": "#!/usr/bin/env bash\nset -euo pipefail\necho ran >> runs.log\n",
	})

	err := runIteration(args{once: true, pollSeconds: 300, at: "2026-03-07T22:15:00Z"}, paths)
	if err != nil {
		t.Fatal(err)
	}

	content, err := os.ReadFile(filepath.Join(paths.repoRoot, "runs.log"))
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(content)); got != "ran" {
		t.Fatalf("expected shell task to append one run, got %q", got)
	}

	runs := readRuns(t, paths)
	if len(runs) != 1 {
		t.Fatalf("expected one task_runs row, got %d", len(runs))
	}
	if runs[0].taskID != "record-success" || runs[0].scheduledFor != "2026-03-07T22:15:00Z" || runs[0].status != "success" {
		t.Fatalf("unexpected run row: %+v", runs[0])
	}
	if got := readStateValue(t, paths, stateKeyLastChecked); got != "2026-03-07T22:15:00Z" {
		t.Fatalf("expected last_checked_at to advance, got %s", got)
	}
}

func TestRunIterationDoesNotDuplicateRecordedSlot(t *testing.T) {
	paths := testRepo(t, `tasks:
  - id: no-duplicate
    enabled: true
    schedule: "15 23 * * *"
    missed: run-latest
    kind: shell
    command: "./scripts/record.sh"
`, map[string]string{
		"record.sh": "#!/usr/bin/env bash\nset -euo pipefail\necho ran >> runs.log\n",
	})
	iterationArgs := args{once: true, pollSeconds: 300, at: "2026-03-07T22:15:00Z"}
	if err := runIteration(iterationArgs, paths); err != nil {
		t.Fatal(err)
	}
	writeLastCheckedForTest(t, paths, "2026-03-07T22:10:00Z")
	if err := runIteration(iterationArgs, paths); err != nil {
		t.Fatal(err)
	}

	content, err := os.ReadFile(filepath.Join(paths.repoRoot, "runs.log"))
	if err != nil {
		t.Fatal(err)
	}
	if lines := strings.Count(strings.TrimSpace(string(content)), "ran"); lines != 1 {
		t.Fatalf("expected one script execution, got log %q", string(content))
	}
	if runs := readRuns(t, paths); len(runs) != 1 {
		t.Fatalf("expected one task_runs row after duplicate iteration, got %d", len(runs))
	}
}

func TestRunIterationRecordsFailedShellTask(t *testing.T) {
	paths := testRepo(t, `tasks:
  - id: record-failure
    enabled: true
    schedule: "15 23 * * *"
    missed: run-latest
    kind: shell
    command: "./scripts/fail.sh"
`, map[string]string{
		"fail.sh": "#!/usr/bin/env bash\nset -euo pipefail\necho 'boom failure' >&2\nexit 42\n",
	})

	err := runIteration(args{once: true, pollSeconds: 300, at: "2026-03-07T22:15:00Z"}, paths)
	if err != nil {
		t.Fatal(err)
	}

	runs := readRuns(t, paths)
	if len(runs) != 1 {
		t.Fatalf("expected one task_runs row, got %d", len(runs))
	}
	if runs[0].status != "failed" {
		t.Fatalf("expected failed status, got %+v", runs[0])
	}
	if !runs[0].errorText.Valid || !strings.Contains(runs[0].errorText.String, "boom failure") {
		t.Fatalf("expected recorded error to include shell stderr, got %+v", runs[0].errorText)
	}
}

func TestRunIterationRunLatestAfterOfflineGap(t *testing.T) {
	paths := testRepo(t, `tasks:
  - id: hourly-latest
    enabled: true
    schedule: "0 * * * *"
    missed: run-latest
    kind: shell
    command: "./scripts/record.sh"
`, map[string]string{
		"record.sh": "#!/usr/bin/env bash\nset -euo pipefail\necho ran >> runs.log\n",
	})
	writeLastCheckedForTest(t, paths, "2026-03-07T09:00:00Z")

	err := runIteration(args{once: true, pollSeconds: 300, at: "2026-03-07T12:30:00Z"}, paths)
	if err != nil {
		t.Fatal(err)
	}

	runs := readRuns(t, paths)
	if len(runs) != 1 {
		t.Fatalf("expected one latest missed run, got %d", len(runs))
	}
	if got := runs[0].scheduledFor; got != "2026-03-07T12:00:00Z" {
		t.Fatalf("expected only latest missed slot, got %s", got)
	}
}

func TestRunIterationCatchUpAfterOfflineGap(t *testing.T) {
	paths := testRepo(t, `tasks:
  - id: hourly-catch-up
    enabled: true
    schedule: "0 * * * *"
    missed: catch-up
    kind: shell
    command: "./scripts/record.sh"
`, map[string]string{
		"record.sh": "#!/usr/bin/env bash\nset -euo pipefail\necho ran >> runs.log\n",
	})
	writeLastCheckedForTest(t, paths, "2026-03-07T09:30:00Z")

	err := runIteration(args{once: true, pollSeconds: 300, at: "2026-03-07T12:30:00Z"}, paths)
	if err != nil {
		t.Fatal(err)
	}

	runs := readRuns(t, paths)
	if len(runs) != 3 {
		t.Fatalf("expected three catch-up runs, got %d", len(runs))
	}
	want := []string{"2026-03-07T10:00:00Z", "2026-03-07T11:00:00Z", "2026-03-07T12:00:00Z"}
	for i, expected := range want {
		if runs[i].scheduledFor != expected || runs[i].status != "success" {
			t.Fatalf("run %d: expected %s success, got %+v", i, expected, runs[i])
		}
	}
}

func TestDryRunDoesNotCreateRuntimeDatabase(t *testing.T) {
	paths := testRepo(t, `tasks:
  - id: dry-run
    enabled: true
    schedule: "15 23 * * *"
    missed: run-latest
    kind: shell
    command: "./scripts/record.sh"
`, map[string]string{
		"record.sh": "#!/usr/bin/env bash\nset -euo pipefail\necho ran >> runs.log\n",
	})

	err := runIteration(args{once: true, dryRun: true, pollSeconds: 300, at: "2026-03-07T22:15:00Z"}, paths)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(paths.dbFile); !os.IsNotExist(err) {
		t.Fatalf("expected dry-run not to create DB, stat err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(paths.repoRoot, "runs.log")); !os.IsNotExist(err) {
		t.Fatalf("expected dry-run not to execute shell task, stat err=%v", err)
	}
}

func TestRunIterationRecordsSuccessfulAgentTask(t *testing.T) {
	paths := testRepo(t, `tasks:
  - id: codex-agent
    enabled: true
    schedule: "15 23 * * *"
    missed: run-latest
    kind: codex
    model: gpt-5.3-codex
    instruction: "Summarize YYYY-MM-DD"
`, nil)
	binDir := filepath.Join(paths.repoRoot, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	fakeCodex := filepath.Join(binDir, "codex")
	if err := os.WriteFile(fakeCodex, []byte("#!/usr/bin/env bash\nset -euo pipefail\nprintf '%s\\n' \"$@\" > agent.args\nprintf '%s\\n' \"${CODEX_TASK_MODEL:-}\" > agent.model\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	err := runIteration(args{once: true, pollSeconds: 300, at: "2026-03-07T22:15:00Z"}, paths)
	if err != nil {
		t.Fatal(err)
	}

	argsContent, err := os.ReadFile(filepath.Join(paths.repoRoot, "agent.args"))
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(argsContent)); got != "exec\n--model\ngpt-5.3-codex\nSummarize 2026-03-07" {
		t.Fatalf("unexpected agent args: %q", got)
	}
	modelContent, err := os.ReadFile(filepath.Join(paths.repoRoot, "agent.model"))
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(modelContent)); got != "gpt-5.3-codex" {
		t.Fatalf("expected CODEX_TASK_MODEL to be exported, got %q", got)
	}

	runs := readRuns(t, paths)
	if len(runs) != 1 || runs[0].status != "success" {
		t.Fatalf("expected one successful agent run, got %+v", runs)
	}
}

func TestRunIterationPassesOpencodeThinkingVariant(t *testing.T) {
	paths := testRepo(t, `tasks:
  - id: opencode-thinking
    enabled: true
    schedule: "15 23 * * *"
    missed: run-latest
    kind: opencode
    model: github-copilot/gpt-5.5
    thinking: medium
    instruction: "Summarize YYYY-MM-DD"
`, nil)
	binDir := filepath.Join(paths.repoRoot, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	fakeOpencode := filepath.Join(binDir, "opencode")
	if err := os.WriteFile(fakeOpencode, []byte("#!/usr/bin/env bash\nset -euo pipefail\nprintf '%s\\n' \"$@\" > opencode.args\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	err := runIteration(args{once: true, pollSeconds: 300, at: "2026-03-07T22:15:00Z"}, paths)
	if err != nil {
		t.Fatal(err)
	}

	argsContent, err := os.ReadFile(filepath.Join(paths.repoRoot, "opencode.args"))
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(argsContent)); got != "run\n-m\ngithub-copilot/gpt-5.5\n--variant\nmedium\nSummarize 2026-03-07" {
		t.Fatalf("unexpected opencode args: %q", got)
	}
}
