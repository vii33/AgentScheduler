package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"math"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"gopkg.in/yaml.v3"
	_ "modernc.org/sqlite"
)

const (
	defaultModel                   = "zen/minimax2.5-free"
	fallbackModel                  = "opencode/minimax-m2.5-free"
	stateKeyLastChecked            = "last_checked_at"
	dryRunInstructionPreviewLength = 80
)

type args struct {
	once        bool
	dryRun      bool
	pollSeconds int
	model       string
	at          string
}

type taskFile struct {
	Tasks []task `yaml:"tasks"`
}

type task struct {
	ID          string `yaml:"id"`
	Enabled     *bool  `yaml:"enabled"`
	Schedule    string `yaml:"schedule"`
	Missed      string `yaml:"missed"`
	Kind        string `yaml:"kind"`
	Command     string `yaml:"command"`
	Instruction string `yaml:"instruction"`
}

type lockFile struct {
	PID       int    `json:"pid"`
	StartedAt string `json:"started_at"`
}

type cronSchedule struct {
	minute cronField
	hour   cronField
	dom    cronField
	month  cronField
	dow    cronField
}

type cronField struct {
	values map[int]bool
}

type candidateRun struct {
	task         task
	scheduledFor time.Time
}

type repoPaths struct {
	repoRoot  string
	tasksFile string
	dbFile    string
	lockFile  string
}

func main() {
	log.SetFlags(0)

	parsedArgs, err := parseArgs()
	if err != nil {
		log.Fatalf("[task-loop] %v", err)
	}

	paths, err := findRepoPaths()
	if err != nil {
		log.Fatalf("[task-loop] %v", err)
	}

	if parsedArgs.once {
		if err := runIteration(parsedArgs, paths); err != nil {
			log.Fatalf("[task-loop] %v", err)
		}
		return
	}

	lock, err := acquireSchedulerLock(paths)
	if err != nil {
		log.Fatalf("[task-loop] %v", err)
	}
	defer releaseSchedulerLock(paths, lock)

	installSignalCleanup(paths, lock)

	log.Printf("[task-loop] started poll=%ds model=%s lock=%s", parsedArgs.pollSeconds, parsedArgs.model, paths.lockFile)
	if err := runIteration(parsedArgs, paths); err != nil {
		log.Printf("[task-loop] %v", err)
	}

	ticker := time.NewTicker(time.Duration(parsedArgs.pollSeconds) * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		if err := runIteration(parsedArgs, paths); err != nil {
			log.Printf("[task-loop] %v", err)
		}
	}
}

func parseArgs() (args, error) {
	pollDefault := 300
	if env := os.Getenv("TASK_LOOP_POLL_SECONDS"); env != "" {
		value, err := strconv.Atoi(env)
		if err != nil || value <= 0 {
			return args{}, errors.New("TASK_LOOP_POLL_SECONDS must be a positive integer")
		}
		pollDefault = value
	}

	modelDefault := os.Getenv("OPENCODE_TASK_MODEL")
	if modelDefault == "" {
		modelDefault = defaultModel
	}

	parsed := args{}
	flag.BoolVar(&parsed.once, "once", false, "run one scheduler iteration and exit")
	flag.BoolVar(&parsed.dryRun, "dry-run", false, "do not execute tasks or modify runtime DB")
	flag.IntVar(&parsed.pollSeconds, "poll-seconds", pollDefault, "loop interval in seconds")
	flag.StringVar(&parsed.model, "model", modelDefault, "OpenCode model for opencode tasks")
	flag.StringVar(&parsed.at, "at", "", "simulate time for one iteration (RFC3339/ISO time)")
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "Usage: go run ./cmd/task-loop [options]\n\nOptions:\n")
		flag.PrintDefaults()
	}
	flag.Parse()

	if parsed.pollSeconds <= 0 {
		return args{}, errors.New("--poll-seconds must be a positive number")
	}
	return parsed, nil
}

func findRepoPaths() (repoPaths, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return repoPaths{}, err
	}
	root := cwd
	for {
		if fileExists(filepath.Join(root, "crons", "tasks.yaml")) {
			return repoPaths{
				repoRoot:  root,
				tasksFile: filepath.Join(root, "crons", "tasks.yaml"),
				dbFile:    filepath.Join(root, "miniclaw.db"),
				lockFile:  filepath.Join(root, "task-loop.lock"),
			}, nil
		}
		parent := filepath.Dir(root)
		if parent == root {
			return repoPaths{}, errors.New("could not find repo root containing crons/tasks.yaml")
		}
		root = parent
	}
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func openDB(paths repoPaths) (*sql.DB, error) {
	db, err := sql.Open("sqlite", paths.dbFile)
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(`PRAGMA journal_mode = WAL; PRAGMA busy_timeout = 5000;`); err != nil {
		db.Close()
		return nil, err
	}
	if err := migrateDB(db); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

func migrateDB(db *sql.DB) error {
	_, err := db.Exec(`
CREATE TABLE IF NOT EXISTS task_runs (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  task_id TEXT NOT NULL,
  scheduled_for TEXT NOT NULL,
  started_at TEXT NOT NULL,
  finished_at TEXT,
  status TEXT NOT NULL CHECK (status IN ('running', 'success', 'failed', 'skipped')),
  error TEXT,
  duration_ms INTEGER,
  UNIQUE(task_id, scheduled_for)
);
CREATE INDEX IF NOT EXISTS idx_task_runs_task_scheduled
  ON task_runs(task_id, scheduled_for DESC);
CREATE INDEX IF NOT EXISTS idx_task_runs_status_started
  ON task_runs(status, started_at DESC);
CREATE TABLE IF NOT EXISTS scheduler_state (
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL
);
`)
	return err
}

func readTasks(paths repoPaths) ([]task, error) {
	content, err := os.ReadFile(paths.tasksFile)
	if err != nil {
		return nil, fmt.Errorf("tasks file not found: %s", paths.tasksFile)
	}
	parsed := taskFile{}
	if err := yaml.Unmarshal(content, &parsed); err != nil {
		return nil, fmt.Errorf("invalid tasks file: %w", err)
	}
	if parsed.Tasks == nil {
		return nil, errors.New("invalid tasks file: expected a top-level 'tasks' array")
	}

	enabledTasks := []task{}
	seen := map[string]bool{}
	for index, t := range parsed.Tasks {
		if err := validateTask(t, index, seen); err != nil {
			return nil, err
		}
		seen[t.ID] = true
		if t.Enabled == nil || *t.Enabled {
			if t.Missed == "" {
				t.Missed = "run-latest"
			}
			enabledTasks = append(enabledTasks, t)
		}
	}
	return enabledTasks, nil
}

func validateTask(t task, index int, seen map[string]bool) error {
	label := fmt.Sprintf("index %d", index)
	if strings.TrimSpace(t.ID) != "" {
		label = t.ID
	}
	if strings.TrimSpace(t.ID) == "" {
		return fmt.Errorf("task at index %d is missing a valid 'id' field", index)
	}
	if seen[t.ID] {
		return fmt.Errorf("task '%s' duplicates an earlier task id", t.ID)
	}
	if strings.TrimSpace(t.Schedule) == "" {
		return fmt.Errorf("task '%s' is missing a valid 'schedule' field", label)
	}
	if _, err := parseCron(t.Schedule); err != nil {
		return fmt.Errorf("task '%s' has invalid schedule: %w", label, err)
	}
	missed := t.Missed
	if missed == "" {
		missed = "run-latest"
	}
	if missed != "skip" && missed != "run-latest" && missed != "catch-up" {
		return fmt.Errorf("task '%s' has invalid missed policy '%s': must be skip, run-latest, or catch-up", label, missed)
	}
	if t.Kind != "shell" && t.Kind != "opencode" {
		return fmt.Errorf("task '%s' has invalid kind '%s': must be 'shell' or 'opencode'", label, t.Kind)
	}
	if t.Kind == "shell" && strings.TrimSpace(t.Command) == "" {
		return fmt.Errorf("task '%s' has kind=shell but no valid 'command' field", label)
	}
	if t.Kind == "opencode" && strings.TrimSpace(t.Instruction) == "" {
		return fmt.Errorf("task '%s' has kind=opencode but no valid 'instruction' field", label)
	}
	return nil
}

func parseCron(schedule string) (cronSchedule, error) {
	fields := strings.Fields(schedule)
	if len(fields) != 5 {
		return cronSchedule{}, errors.New("expected 5 fields")
	}
	minute, err := parseCronField(fields[0], 0, 59)
	if err != nil {
		return cronSchedule{}, fmt.Errorf("minute: %w", err)
	}
	hour, err := parseCronField(fields[1], 0, 23)
	if err != nil {
		return cronSchedule{}, fmt.Errorf("hour: %w", err)
	}
	dom, err := parseCronField(fields[2], 1, 31)
	if err != nil {
		return cronSchedule{}, fmt.Errorf("day-of-month: %w", err)
	}
	month, err := parseCronField(fields[3], 1, 12)
	if err != nil {
		return cronSchedule{}, fmt.Errorf("month: %w", err)
	}
	dow, err := parseCronField(fields[4], 0, 7)
	if err != nil {
		return cronSchedule{}, fmt.Errorf("day-of-week: %w", err)
	}
	if dow.values[7] {
		dow.values[0] = true
		delete(dow.values, 7)
	}
	return cronSchedule{minute: minute, hour: hour, dom: dom, month: month, dow: dow}, nil
}

func parseCronField(expr string, min int, max int) (cronField, error) {
	field := cronField{values: map[int]bool{}}
	for _, rawPart := range strings.Split(expr, ",") {
		part := strings.TrimSpace(rawPart)
		if part == "" {
			return cronField{}, errors.New("empty list item")
		}
		rangePart := part
		step := 1
		if strings.Contains(part, "/") {
			pieces := strings.Split(part, "/")
			if len(pieces) != 2 || pieces[0] == "" || pieces[1] == "" {
				return cronField{}, fmt.Errorf("invalid step expression '%s'", part)
			}
			rangePart = pieces[0]
			parsedStep, err := strconv.Atoi(pieces[1])
			if err != nil || parsedStep <= 0 {
				return cronField{}, fmt.Errorf("invalid step in '%s'", part)
			}
			step = parsedStep
		}

		start, end, err := parseCronRange(rangePart, min, max)
		if err != nil {
			return cronField{}, err
		}
		for value := start; value <= end; value += step {
			field.values[value] = true
		}
	}
	return field, nil
}

func parseCronRange(expr string, min int, max int) (int, int, error) {
	if expr == "*" {
		return min, max, nil
	}
	if strings.Contains(expr, "-") {
		pieces := strings.Split(expr, "-")
		if len(pieces) != 2 {
			return 0, 0, fmt.Errorf("invalid range '%s'", expr)
		}
		start, err1 := strconv.Atoi(pieces[0])
		end, err2 := strconv.Atoi(pieces[1])
		if err1 != nil || err2 != nil || start > end || start < min || end > max {
			return 0, 0, fmt.Errorf("invalid range '%s'", expr)
		}
		return start, end, nil
	}
	value, err := strconv.Atoi(expr)
	if err != nil || value < min || value > max {
		return 0, 0, fmt.Errorf("invalid value '%s'", expr)
	}
	return value, value, nil
}

func (s cronSchedule) matches(t time.Time) bool {
	utc := t.UTC()
	return s.minute.values[utc.Minute()] &&
		s.hour.values[utc.Hour()] &&
		s.dom.values[utc.Day()] &&
		s.month.values[int(utc.Month())] &&
		s.dow.values[int(utc.Weekday())]
}

func runIteration(parsedArgs args, paths repoPaths) error {
	tasks, err := readTasks(paths)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	if parsedArgs.at != "" {
		parsedAt, err := parseTimeArg(parsedArgs.at)
		if err != nil {
			return err
		}
		now = parsedAt
	}

	var db *sql.DB
	var lastChecked time.Time
	if parsedArgs.dryRun {
		lastChecked = now.Add(-time.Duration(parsedArgs.pollSeconds) * time.Second)
	} else {
		var err error
		db, err = openDB(paths)
		if err != nil {
			return err
		}
		defer db.Close()

		lastChecked, err = readLastChecked(db)
		if err != nil {
			return err
		}
		if lastChecked.IsZero() || lastChecked.After(now) {
			lastChecked = now.Add(-time.Duration(parsedArgs.pollSeconds) * time.Second)
		}
	}

	due, err := findDueRuns(tasks, lastChecked, now, time.Duration(parsedArgs.pollSeconds)*time.Second)
	if err != nil {
		return err
	}
	if len(due) == 0 {
		log.Printf("[task-loop] %s no due tasks", formatTime(now))
		if !parsedArgs.dryRun {
			return writeLastChecked(db, now)
		}
		return nil
	}

	labels := make([]string, 0, len(due))
	for _, run := range due {
		labels = append(labels, fmt.Sprintf("%s@%s", run.task.ID, formatTime(run.scheduledFor)))
	}
	log.Printf("[task-loop] %s due runs: %s", formatTime(now), strings.Join(labels, ", "))

	resolvedModel := ""
	getModel := func() string {
		if resolvedModel == "" {
			resolvedModel = resolveModel(parsedArgs.model, paths.repoRoot)
		}
		return resolvedModel
	}

	for _, run := range due {
		if parsedArgs.dryRun {
			logDryRun(run, now)
			continue
		}

		runID, inserted, err := insertRunningRun(db, run.task.ID, run.scheduledFor, now)
		if err != nil {
			return err
		}
		if !inserted {
			log.Printf("[task-loop] skip %s scheduled_for=%s already recorded", run.task.ID, formatTime(run.scheduledFor))
			continue
		}

		log.Printf("[task-loop] running: %s scheduled_for=%s", run.task.ID, formatTime(run.scheduledFor))
		started := time.Now().UTC()
		err = executeTask(run.task, getModel, run.scheduledFor, paths.repoRoot)
		finished := time.Now().UTC()
		durationMs := int(finished.Sub(started).Milliseconds())
		if err != nil {
			message := sanitizeError(err)
			log.Printf("[task-loop] failed %s: %s", run.task.ID, message)
			if updateErr := finishRun(db, runID, "failed", finished, durationMs, message); updateErr != nil {
				return updateErr
			}
			continue
		}
		if err := finishRun(db, runID, "success", finished, durationMs, ""); err != nil {
			return err
		}
	}

	if !parsedArgs.dryRun {
		return writeLastChecked(db, now)
	}
	return nil
}

func parseTimeArg(value string) (time.Time, error) {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err == nil {
		return parsed.UTC(), nil
	}
	return time.Time{}, errors.New("--at must be an RFC3339/ISO date/time string, e.g. 2026-03-07T23:15:00Z")
}

func readLastChecked(db *sql.DB) (time.Time, error) {
	var value string
	err := db.QueryRow(`SELECT value FROM scheduler_state WHERE key = ?`, stateKeyLastChecked).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return time.Time{}, nil
	}
	if err != nil {
		return time.Time{}, err
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid scheduler_state.%s value: %w", stateKeyLastChecked, err)
	}
	return parsed.UTC(), nil
}

func writeLastChecked(db *sql.DB, checkedAt time.Time) error {
	_, err := db.Exec(`
INSERT INTO scheduler_state(key, value) VALUES(?, ?)
ON CONFLICT(key) DO UPDATE SET value = excluded.value
`, stateKeyLastChecked, formatTime(checkedAt))
	return err
}

func findDueRuns(tasks []task, lastChecked time.Time, now time.Time, pollInterval time.Duration) ([]candidateRun, error) {
	if !lastChecked.Before(now) {
		return nil, nil
	}
	due := []candidateRun{}
	freshCutoff := now.Add(-pollInterval)

	for _, t := range tasks {
		schedule, err := parseCron(t.Schedule)
		if err != nil {
			return nil, fmt.Errorf("task '%s' has invalid schedule: %w", t.ID, err)
		}
		slots := matchingSlots(schedule, lastChecked, now)
		if len(slots) == 0 {
			continue
		}

		switch t.Missed {
		case "skip":
			for _, slot := range slots {
				if !slot.Before(freshCutoff) {
					due = append(due, candidateRun{task: t, scheduledFor: slot})
				}
			}
		case "catch-up":
			for _, slot := range slots {
				due = append(due, candidateRun{task: t, scheduledFor: slot})
			}
		case "run-latest", "":
			due = append(due, candidateRun{task: t, scheduledFor: slots[len(slots)-1]})
		default:
			return nil, fmt.Errorf("task '%s' has invalid missed policy '%s'", t.ID, t.Missed)
		}
	}
	return due, nil
}

func matchingSlots(schedule cronSchedule, after time.Time, through time.Time) []time.Time {
	start := after.UTC().Truncate(time.Minute).Add(time.Minute)
	end := through.UTC().Truncate(time.Minute)
	if end.After(through.UTC()) {
		end = end.Add(-time.Minute)
	}

	slots := []time.Time{}
	for current := start; !current.After(end); current = current.Add(time.Minute) {
		if schedule.matches(current) {
			slots = append(slots, current)
		}
	}
	return slots
}

func insertRunningRun(db *sql.DB, taskID string, scheduledFor time.Time, startedAt time.Time) (int64, bool, error) {
	result, err := db.Exec(`
INSERT OR IGNORE INTO task_runs(task_id, scheduled_for, started_at, status)
VALUES(?, ?, ?, 'running')
`, taskID, formatTime(scheduledFor), formatTime(startedAt))
	if err != nil {
		return 0, false, err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return 0, false, err
	}
	if changed == 0 {
		return 0, false, nil
	}
	id, err := result.LastInsertId()
	if err != nil {
		return 0, false, err
	}
	return id, true, nil
}

func finishRun(db *sql.DB, runID int64, status string, finishedAt time.Time, durationMs int, message string) error {
	var errorValue any = nil
	if message != "" {
		errorValue = message
	}
	_, err := db.Exec(`
UPDATE task_runs
SET finished_at = ?, status = ?, duration_ms = ?, error = ?
WHERE id = ?
`, formatTime(finishedAt), status, durationMs, errorValue, runID)
	return err
}

func logDryRun(run candidateRun, now time.Time) {
	renderedText := ""
	detail := ""
	if run.task.Kind == "shell" {
		renderedText = applyTaskPlaceholders(run.task.Command, run.scheduledFor)
		detail = fmt.Sprintf("command: %s", renderedText)
	} else {
		renderedText = applyTaskPlaceholders(run.task.Instruction, run.scheduledFor)
		preview := strings.ReplaceAll(renderedText, "\n", " ")
		if len(preview) > dryRunInstructionPreviewLength {
			preview = preview[:dryRunInstructionPreviewLength] + "..."
		}
		detail = fmt.Sprintf("instruction: %s", preview)
	}
	log.Printf("[task-loop] dry-run %s scheduled_for=%s now=%s (%s) — %s", run.task.ID, formatTime(run.scheduledFor), formatTime(now), run.task.Kind, detail)
}

func executeTask(t task, getModel func() string, scheduledFor time.Time, repoRoot string) error {
	if t.Kind == "shell" {
		cmd := applyTaskPlaceholders(t.Command, scheduledFor)
		if strings.TrimSpace(cmd) == "" {
			return fmt.Errorf("task '%s' has kind=shell but no command defined", t.ID)
		}
		if !shellCommandAllowed(cmd) {
			return fmt.Errorf("rejected unsafe shell command: %s", cmd)
		}
		tokens, err := splitShellWords(cmd)
		if err != nil {
			return err
		}
		if len(tokens) == 0 {
			return fmt.Errorf("task '%s' has empty shell command", t.ID)
		}
		result := runCommand(tokens[0], tokens[1:], repoRoot)
		if result.code != 0 {
			return fmt.Errorf("shell task failed: %s", firstNonEmpty(result.stderr, result.stdout))
		}
		return nil
	}

	if t.Kind == "opencode" {
		instruction := strings.TrimSpace(applyTaskPlaceholders(t.Instruction, scheduledFor))
		if instruction == "" {
			return fmt.Errorf("task '%s' has kind=opencode but no instruction defined", t.ID)
		}
		result := runCommand("opencode", []string{"run", "-m", getModel(), instruction}, repoRoot)
		if result.code != 0 {
			return fmt.Errorf("OpenCode task failed: %s", firstNonEmpty(result.stderr, result.stdout))
		}
		return nil
	}

	return fmt.Errorf("task '%s' has unknown kind: %s", t.ID, t.Kind)
}

type commandResult struct {
	code   int
	stdout string
	stderr string
}

func runCommand(command string, commandArgs []string, cwd string) commandResult {
	cmd := exec.Command(command, commandArgs...)
	cmd.Dir = cwd
	cmd.Env = os.Environ()
	stdout, stderr := strings.Builder{}, strings.Builder{}
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	code := 0
	if err != nil {
		code = 1
		var exitError *exec.ExitError
		if errors.As(err, &exitError) {
			code = exitError.ExitCode()
		}
	}
	return commandResult{code: code, stdout: stdout.String(), stderr: stderr.String()}
}

func resolveModel(preferred string, repoRoot string) string {
	list := runCommand("opencode", []string{"models"}, repoRoot)
	if list.code != 0 {
		return preferred
	}
	models := map[string]bool{}
	for _, line := range strings.Split(list.stdout, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			models[line] = true
		}
	}
	if models[preferred] {
		return preferred
	}
	if preferred == defaultModel && models[fallbackModel] {
		log.Printf("[task-loop] model fallback: %s -> %s", defaultModel, fallbackModel)
		return fallbackModel
	}
	return preferred
}

func shellCommandAllowed(cmd string) bool {
	normalized := strings.TrimSpace(cmd)
	return strings.HasPrefix(normalized, "./scripts/") ||
		strings.HasPrefix(normalized, "scripts/") ||
		strings.HasPrefix(normalized, "bash scripts/") ||
		strings.HasPrefix(normalized, "node scripts/") ||
		strings.HasPrefix(normalized, "go run ./cmd/")
}

func splitShellWords(command string) ([]string, error) {
	args := []string{}
	current := strings.Builder{}
	quote := rune(0)
	escaping := false

	for _, char := range command {
		if escaping {
			current.WriteRune(char)
			escaping = false
			continue
		}
		if char == '\\' {
			escaping = true
			continue
		}
		if quote != 0 {
			if char == quote {
				quote = 0
			} else {
				current.WriteRune(char)
			}
			continue
		}
		if char == '\'' || char == '"' {
			quote = char
			continue
		}
		if char == ' ' || char == '\t' || char == '\n' || char == '\r' {
			if current.Len() > 0 {
				args = append(args, current.String())
				current.Reset()
			}
			continue
		}
		current.WriteRune(char)
	}
	if escaping {
		return nil, fmt.Errorf("rejected shell command with trailing escape: %s", command)
	}
	if quote != 0 {
		return nil, fmt.Errorf("rejected shell command with unterminated quote: %s", command)
	}
	if current.Len() > 0 {
		args = append(args, current.String())
	}
	return args, nil
}

func applyTaskPlaceholders(text string, scheduledFor time.Time) string {
	return strings.ReplaceAll(strings.ReplaceAll(text, "YYYY-MM-DD", scheduledFor.UTC().Format("2006-01-02")), "YYYY-Www", isoWeek(scheduledFor.UTC()))
}

func isoWeek(t time.Time) string {
	year, week := t.ISOWeek()
	return fmt.Sprintf("%04d-W%02d", year, week)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return "no output"
}

func sanitizeError(err error) string {
	message := strings.Join(strings.Fields(err.Error()), " ")
	if len(message) > 200 {
		message = message[:200]
	}
	return message
}

func formatTime(t time.Time) string {
	return t.UTC().Format(time.RFC3339Nano)
}

func acquireSchedulerLock(paths repoPaths) (lockFile, error) {
	lock := lockFile{PID: os.Getpid(), StartedAt: formatTime(time.Now().UTC())}
	content, err := json.MarshalIndent(lock, "", "  ")
	if err != nil {
		return lockFile{}, err
	}
	content = append(content, '\n')

	for attempt := 0; attempt < 2; attempt++ {
		file, err := os.OpenFile(paths.lockFile, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if err == nil {
			_, writeErr := file.Write(content)
			closeErr := file.Close()
			if writeErr != nil {
				return lockFile{}, writeErr
			}
			if closeErr != nil {
				return lockFile{}, closeErr
			}
			return lock, nil
		}
		if !errors.Is(err, fs.ErrExist) {
			return lockFile{}, err
		}
		existing := readLock(paths.lockFile)
		if processIsLive(existing.PID) {
			return lockFile{}, fmt.Errorf("refusing to start: live scheduler already owns %s (%s)", paths.lockFile, describeLock(existing))
		}
		log.Printf("[task-loop] removing stale lock: %s (%s)", paths.lockFile, describeLock(existing))
		if err := os.Remove(paths.lockFile); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return lockFile{}, err
		}
	}
	return lockFile{}, fmt.Errorf("could not acquire scheduler lock: %s", paths.lockFile)
}

func readLock(path string) lockFile {
	content, err := os.ReadFile(path)
	if err != nil {
		return lockFile{}
	}
	lock := lockFile{}
	if err := json.Unmarshal(content, &lock); err != nil {
		return lockFile{}
	}
	return lock
}

func describeLock(lock lockFile) string {
	if lock.PID == 0 {
		return "unknown owner"
	}
	if lock.StartedAt == "" {
		return fmt.Sprintf("pid=%d", lock.PID)
	}
	return fmt.Sprintf("pid=%d started_at=%s", lock.PID, lock.StartedAt)
}

func processIsLive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

func releaseSchedulerLock(paths repoPaths, lock lockFile) {
	current := readLock(paths.lockFile)
	if current.PID != lock.PID || current.StartedAt != lock.StartedAt {
		return
	}
	if err := os.Remove(paths.lockFile); err != nil && !errors.Is(err, fs.ErrNotExist) {
		log.Printf("[task-loop] Warning: could not remove lock %s: %v", paths.lockFile, err)
	}
}

func installSignalCleanup(paths repoPaths, lock lockFile) {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	go func() {
		sig := <-ch
		releaseSchedulerLock(paths, lock)
		if signalValue, ok := sig.(syscall.Signal); ok {
			os.Exit(128 + int(signalValue))
		}
		os.Exit(1)
	}()
}

func init() {
	if math.MaxInt < 2147483647 {
		panic("unsupported platform")
	}
}
