// scheduler is a Go rewrite of scripts/task-loop.js.
// It reads task definitions from crons/tasks.yaml, tracks runtime state in
// .miniclaw/task-state.json, and executes due tasks on a polling loop.
//
// Usage:
//
//	go run .                          # poll every 60 s (default)
//	go run . --once                   # single iteration and exit
//	go run . --once --dry-run         # print due tasks without running them
//	go run . --once --at 2026-03-07T23:00:00Z  # simulate a specific time
//	go run . --poll-seconds 30        # custom poll interval
//	go run . --model provider/model   # custom OpenCode model
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	defaultModel       = "zen/minimax2.5-free"
	defaultPollSeconds = 60
)

// Task is one entry from crons/tasks.yaml.
type Task struct {
	ID             string `yaml:"id"`
	Enabled        bool   `yaml:"enabled"`
	Schedule       string `yaml:"schedule"`
	Kind           string `yaml:"kind"`        // "shell" or "opencode"
	Command        string `yaml:"command"`     // used when kind=shell
	Instruction    string `yaml:"instruction"` // used when kind=opencode
	TimeoutSeconds int    `yaml:"timeout_seconds"`
	AllowOverlap   bool   `yaml:"allow_overlap"`
	MaxRetries     int    `yaml:"max_retries"`
	Description    string `yaml:"description"`
}

// tasksConfig is the root of crons/tasks.yaml.
type tasksConfig struct {
	Tasks []Task `yaml:"tasks"`
}

// TaskState is the runtime metadata for a single task stored in task-state.json.
type TaskState struct {
	LastRun     *string `json:"last_run"`
	LastSuccess *string `json:"last_success"`
	LastError   *string `json:"last_error"`
	Running     bool    `json:"running"`
}

// StateFile maps task IDs to their mutable runtime state.
type StateFile map[string]*TaskState

// config holds parsed CLI flags.
type config struct {
	once        bool
	dryRun      bool
	pollSeconds int
	model       string
	at          *time.Time
	repoRoot    string
}

func main() {
	cfg := parseArgs()

	root, err := findRepoRoot()
	if err != nil {
		log.Fatalf("[scheduler] cannot find repo root: %v", err)
	}
	cfg.repoRoot = root

	tasksFile := filepath.Join(root, "crons", "tasks.yaml")
	stateFile := filepath.Join(root, ".miniclaw", "task-state.json")

	cfg.model = resolveModel(cfg.model, root)

	if cfg.once {
		runIteration(cfg, tasksFile, stateFile)
		return
	}

	log.Printf("[scheduler] started poll=%ds model=%s", cfg.pollSeconds, cfg.model)
	runIteration(cfg, tasksFile, stateFile)

	ticker := time.NewTicker(time.Duration(cfg.pollSeconds) * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		runIteration(cfg, tasksFile, stateFile)
	}
}

// parseArgs parses command-line flags and returns a config.
func parseArgs() config {
	cfg := config{
		pollSeconds: defaultPollSeconds,
		model:       envOrDefault("OPENCODE_TASK_MODEL", defaultModel),
	}

	once := flag.Bool("once", false, "Run one scheduler iteration and exit")
	dryRun := flag.Bool("dry-run", false, "Do not execute tasks or modify files")
	pollSeconds := flag.Int("poll-seconds", cfg.pollSeconds, "Loop interval in seconds")
	model := flag.String("model", cfg.model, "OpenCode model for task execution")
	atStr := flag.String("at", "", "Simulate a specific time (ISO 8601); implies --once")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: scheduler [options]\n\nOptions:\n")
		flag.PrintDefaults()
	}
	flag.Parse()

	cfg.once = *once
	cfg.dryRun = *dryRun
	cfg.pollSeconds = *pollSeconds
	cfg.model = *model

	if *atStr != "" {
		t, err := time.Parse(time.RFC3339, *atStr)
		if err != nil {
			log.Fatalf("[scheduler] --at must be an RFC3339 timestamp (e.g. 2026-03-07T23:00:00Z): %v", err)
		}
		cfg.at = &t
		cfg.once = true
	}

	return cfg
}

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// findRepoRoot walks up from the working directory until it finds a .git directory.
func findRepoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	// Fall back to working directory if no .git is found.
	return os.Getwd()
}

// loadTasks reads and parses crons/tasks.yaml.
func loadTasks(tasksFile string) ([]Task, error) {
	data, err := os.ReadFile(tasksFile)
	if err != nil {
		return nil, fmt.Errorf("read tasks file: %w", err)
	}
	var cfg tasksConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse tasks yaml: %w", err)
	}
	return cfg.Tasks, nil
}

// loadState reads .miniclaw/task-state.json; returns an empty map if missing.
func loadState(stateFile string) (StateFile, error) {
	state := make(StateFile)
	data, err := os.ReadFile(stateFile)
	if os.IsNotExist(err) {
		return state, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read state file: %w", err)
	}
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("parse state json: %w", err)
	}
	return state, nil
}

// saveState writes the state map to disk atomically.
func saveState(stateFile string, state StateFile) error {
	if err := os.MkdirAll(filepath.Dir(stateFile), 0o755); err != nil {
		return fmt.Errorf("create state dir: %w", err)
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal state: %w", err)
	}
	tmp := stateFile + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write state tmp: %w", err)
	}
	return os.Rename(tmp, stateFile)
}

// minuteKey returns a string that is equal for two times in the same minute.
func minuteKey(t time.Time) string {
	return t.UTC().Truncate(time.Minute).Format(time.RFC3339)
}

// alreadyRanThisSlot returns true if the task already ran in the current minute.
func alreadyRanThisSlot(state *TaskState, now time.Time) bool {
	if state == nil || state.LastRun == nil {
		return false
	}
	lastRun, err := time.Parse(time.RFC3339, *state.LastRun)
	if err != nil {
		return false
	}
	return minuteKey(lastRun) == minuteKey(now)
}

// cronMatches reports whether the 5-field cron expression matches t.
func cronMatches(schedule string, t time.Time) bool {
	fields := strings.Fields(schedule)
	if len(fields) != 5 {
		return false
	}
	return fieldMatches(fields[0], t.Minute(), 0, 59) &&
		fieldMatches(fields[1], t.Hour(), 0, 23) &&
		fieldMatches(fields[2], t.Day(), 1, 31) &&
		fieldMatches(fields[3], int(t.Month()), 1, 12) &&
		fieldMatches(fields[4], int(t.Weekday()), 0, 6)
}

// fieldMatches checks whether value is matched by the cron field expression
// (supports *, ranges, steps, and comma-separated lists).
func fieldMatches(expr string, value, minVal, maxVal int) bool {
	for _, part := range strings.Split(expr, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if matchPart(part, value, minVal, maxVal) {
			return true
		}
	}
	return false
}

func matchPart(part string, value, minVal, maxVal int) bool {
	step := 1
	if idx := strings.Index(part, "/"); idx >= 0 {
		s, err := strconv.Atoi(part[idx+1:])
		if err != nil || s <= 0 {
			return false
		}
		step = s
		part = part[:idx]
	}

	var start, end int
	switch {
	case part == "*":
		start, end = minVal, maxVal
	case strings.Contains(part, "-"):
		bounds := strings.SplitN(part, "-", 2)
		s, err1 := strconv.Atoi(bounds[0])
		e, err2 := strconv.Atoi(bounds[1])
		if err1 != nil || err2 != nil {
			return false
		}
		start, end = s, e
	default:
		n, err := strconv.Atoi(part)
		if err != nil {
			return false
		}
		start, end = n, n
	}

	if value < start || value > end {
		return false
	}
	return (value-start)%step == 0
}

// resolveModel returns the preferred model name, falling back if not available.
func resolveModel(preferred, repoRoot string) string {
	cmd := exec.Command("opencode", "models")
	cmd.Dir = repoRoot
	out, err := cmd.Output()
	if err != nil {
		return preferred
	}
	modelSet := make(map[string]bool)
	for _, line := range strings.Split(string(out), "\n") {
		if m := strings.TrimSpace(line); m != "" {
			modelSet[m] = true
		}
	}
	if modelSet[preferred] {
		return preferred
	}
	if preferred == "zen/minimax2.5-free" && modelSet["opencode/minimax-m2.5-free"] {
		log.Println("[scheduler] model fallback: zen/minimax2.5-free -> opencode/minimax-m2.5-free")
		return "opencode/minimax-m2.5-free"
	}
	return preferred
}

// shellCommandAllowed restricts shell payloads to the scripts/ directory.
func shellCommandAllowed(cmd string) bool {
	n := strings.TrimSpace(cmd)
	return strings.HasPrefix(n, "./scripts/") ||
		strings.HasPrefix(n, "scripts/") ||
		strings.HasPrefix(n, "bash scripts/") ||
		strings.HasPrefix(n, "node scripts/")
}

// runCmd runs a command and returns its combined output.
func runCmd(name string, args []string, dir string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// executeTask runs a single task.
func executeTask(task Task, model, repoRoot string) error {
	switch task.Kind {
	case "shell":
		if !shellCommandAllowed(task.Command) {
			return fmt.Errorf("rejected unsafe shell command: %s", task.Command)
		}
		out, err := runCmd("bash", []string{"-lc", task.Command}, repoRoot)
		if err != nil {
			return fmt.Errorf("shell task failed: %s", strings.TrimSpace(out))
		}
		return nil
	case "opencode":
		out, err := runCmd("opencode", []string{"run", "-m", model, task.Instruction}, repoRoot)
		if err != nil {
			return fmt.Errorf("opencode task failed: %s", strings.TrimSpace(out))
		}
		return nil
	default:
		return fmt.Errorf("unknown task kind: %s", task.Kind)
	}
}

func nowStr() string {
	return time.Now().UTC().Format(time.RFC3339)
}

func strPtr(s string) *string { return &s }

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

// runIteration is one scheduling cycle: load tasks, check due, execute.
func runIteration(cfg config, tasksFile, stateFile string) {
	tasks, err := loadTasks(tasksFile)
	if err != nil {
		log.Printf("[scheduler] failed to load tasks: %v", err)
		return
	}

	state, err := loadState(stateFile)
	if err != nil {
		log.Printf("[scheduler] failed to load state: %v", err)
		return
	}

	now := time.Now().UTC()
	if cfg.at != nil {
		now = cfg.at.UTC()
	}

	var due []Task
	for _, task := range tasks {
		if !task.Enabled {
			continue
		}
		if cronMatches(task.Schedule, now) && !alreadyRanThisSlot(state[task.ID], now) {
			due = append(due, task)
		}
	}

	if len(due) == 0 {
		log.Printf("[scheduler] %s no due tasks", now.Format(time.RFC3339))
		return
	}

	ids := make([]string, len(due))
	for i, t := range due {
		ids[i] = t.ID
	}
	log.Printf("[scheduler] %s due tasks: %s", now.Format(time.RFC3339), strings.Join(ids, ", "))

	for _, task := range due {
		log.Printf("[scheduler] running: %s", task.ID)

		if cfg.dryRun {
			switch task.Kind {
			case "shell":
				log.Printf("[scheduler] dry-run command: %s", task.Command)
			case "opencode":
				preview := truncate(strings.ReplaceAll(task.Instruction, "\n", " "), 120)
				log.Printf("[scheduler] dry-run instruction: %s", preview)
			}
			continue
		}

		startTS := nowStr()
		if err := executeTask(task, cfg.model, cfg.repoRoot); err != nil {
			msg := truncate(strings.ReplaceAll(err.Error(), "\n", " "), 200)
			log.Printf("[scheduler] failed %s: %s", task.ID, msg)

			if state[task.ID] == nil {
				state[task.ID] = &TaskState{}
			}
			state[task.ID].LastRun = strPtr(startTS)
			state[task.ID].LastError = strPtr(fmt.Sprintf("%s — %s", nowStr(), msg))
			state[task.ID].Running = false
		} else {
			ts := nowStr()
			if state[task.ID] == nil {
				state[task.ID] = &TaskState{}
			}
			state[task.ID].LastRun = strPtr(ts)
			state[task.ID].LastSuccess = strPtr(ts)
			state[task.ID].LastError = nil
			state[task.ID].Running = false
			log.Printf("[scheduler] completed: %s", task.ID)
		}

		if !cfg.dryRun {
			if err := saveState(stateFile, state); err != nil {
				log.Printf("[scheduler] failed to save state: %v", err)
			}
		}
	}
}
