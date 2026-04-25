#!/usr/bin/env node

const fs = require("fs");
const path = require("path");
const { spawnSync } = require("child_process");
const yaml = require("js-yaml");

const REPO_ROOT = path.resolve(__dirname, "..");
const TASKS_FILE = path.join(REPO_ROOT, "crons", "tasks.yaml");
const STATE_FILE = path.join(REPO_ROOT, ".miniclaw", "task-state.json");
const DEFAULT_MODEL = process.env.OPENCODE_TASK_MODEL || "zen/minimax2.5-free";
const DEFAULT_POLL_SECONDS = Number(process.env.TASK_LOOP_POLL_SECONDS || 60);
const DRY_RUN_INSTRUCTION_PREVIEW_LENGTH = 80;

function parseArgs(argv) {
  const args = {
    once: false,
    pollSeconds: DEFAULT_POLL_SECONDS,
    model: DEFAULT_MODEL,
    at: null,
    dryRun: false,
  };

  for (let i = 2; i < argv.length; i++) {
    const token = argv[i];
    if (token === "--once") {
      args.once = true;
      continue;
    }
    if (token === "--dry-run") {
      args.dryRun = true;
      continue;
    }
    if (token === "--poll-seconds") {
      const value = Number(argv[++i]);
      if (!Number.isFinite(value) || value <= 0) {
        throw new Error("--poll-seconds must be a positive number");
      }
      args.pollSeconds = value;
      continue;
    }
    if (token === "--model") {
      args.model = argv[++i];
      if (!args.model) throw new Error("--model requires a value");
      continue;
    }
    if (token === "--at") {
      const value = argv[++i];
      const date = new Date(value);
      if (Number.isNaN(date.getTime())) {
        throw new Error("--at must be an ISO date/time string");
      }
      args.at = date;
      continue;
    }
    if (token === "-h" || token === "--help") {
      printHelp();
      process.exit(0);
    }
    throw new Error(`Unknown argument: ${token}`);
  }

  return args;
}

function printHelp() {
  console.log(`Usage: node scripts/task-loop.js [options]\n\nOptions:\n  --once                 Run one scheduler iteration and exit\n  --dry-run              Do not execute tasks or modify files\n  --poll-seconds N       Loop interval in seconds (default: ${DEFAULT_POLL_SECONDS})\n  --model provider/model OpenCode model for task execution\n  --at ISO_TIME          Simulate time for one iteration (testing)\n  -h, --help             Show help`);
}

const SHELL_METACHARACTERS = /[;&|`$><\n\r]/;

function readTasks() {
  if (!fs.existsSync(TASKS_FILE)) {
    throw new Error(`Tasks file not found: ${TASKS_FILE}`);
  }
  const content = fs.readFileSync(TASKS_FILE, "utf8");
  const parsed = yaml.load(content);
  if (!parsed || !Array.isArray(parsed.tasks)) {
    throw new Error(`Invalid tasks file: expected a top-level 'tasks' array`);
  }
  const tasks = parsed.tasks.filter((t) => t && t.enabled !== false);
  tasks.forEach((task, i) => validateTask(task, i));
  return tasks;
}

function validateTask(task, index) {
  if (!task.id || typeof task.id !== "string") {
    throw new Error(`Task at index ${index} is missing a valid 'id' field`);
  }
  if (!task.schedule || typeof task.schedule !== "string") {
    throw new Error(`Task '${task.id}' is missing a valid 'schedule' field`);
  }
  if (task.kind !== "shell" && task.kind !== "opencode") {
    throw new Error(`Task '${task.id}' has invalid kind '${String(task.kind)}': must be 'shell' or 'opencode'`);
  }
  if (task.kind === "shell" && !task.command) {
    throw new Error(`Task '${task.id}' has kind=shell but no 'command' field`);
  }
  if (task.kind === "opencode" && !task.instruction) {
    throw new Error(`Task '${task.id}' has kind=opencode but no 'instruction' field`);
  }
}

function readState() {
  if (!fs.existsSync(STATE_FILE)) {
    return {};
  }
  try {
    return JSON.parse(fs.readFileSync(STATE_FILE, "utf8"));
  } catch (err) {
    console.warn(`[task-loop] Warning: could not parse state file, starting fresh: ${err.message}`);
    return {};
  }
}

function writeState(state) {
  const dir = path.dirname(STATE_FILE);
  if (!fs.existsSync(dir)) {
    fs.mkdirSync(dir, { recursive: true });
  }
  fs.writeFileSync(STATE_FILE, JSON.stringify(state, null, 2) + "\n");
}

function fieldMatches(expr, value, min, max) {
  const parts = expr.split(",").map((p) => p.trim()).filter(Boolean);
  for (const part of parts) {
    if (part === "*") return true;

    let [rangePart, stepPart] = part.split("/");
    const step = stepPart ? Number(stepPart) : 1;
    if (!Number.isFinite(step) || step <= 0) continue;

    let start;
    let end;

    if (rangePart === "*") {
      start = min;
      end = max;
    } else if (rangePart.includes("-")) {
      const [s, e] = rangePart.split("-").map(Number);
      if (!Number.isFinite(s) || !Number.isFinite(e)) continue;
      start = s;
      end = e;
    } else {
      const n = Number(rangePart);
      if (!Number.isFinite(n)) continue;
      start = n;
      end = n;
    }

    if (value < start || value > end) continue;
    if ((value - start) % step === 0) return true;
  }

  return false;
}

function cronMatches(schedule, date) {
  const fields = schedule.trim().split(/\s+/);
  if (fields.length !== 5) return false;

  const [min, hour, dom, month, dow] = fields;

  return (
    fieldMatches(min, date.getMinutes(), 0, 59) &&
    fieldMatches(hour, date.getHours(), 0, 23) &&
    fieldMatches(dom, date.getDate(), 1, 31) &&
    fieldMatches(month, date.getMonth() + 1, 1, 12) &&
    fieldMatches(dow, date.getDay(), 0, 6)
  );
}

function minuteKey(date) {
  const d = new Date(date);
  d.setSeconds(0, 0);
  return d.toISOString();
}

function alreadyRanThisSlot(taskState, now) {
  if (!taskState || !taskState.last_run) return false;
  const lastRun = new Date(taskState.last_run);
  if (Number.isNaN(lastRun.getTime())) return false;
  return minuteKey(lastRun) === minuteKey(now);
}

function runCommand(command, args, cwd) {
  const result = spawnSync(command, args, {
    cwd,
    encoding: "utf8",
    env: process.env,
  });
  return {
    code: result.status ?? 1,
    stdout: result.stdout || "",
    stderr: result.stderr || "",
  };
}

function resolveModel(preferred) {
  const list = runCommand("opencode", ["models"], REPO_ROOT);
  if (list.code !== 0) {
    return preferred;
  }

  const models = list.stdout
    .split("\n")
    .map((line) => line.trim())
    .filter(Boolean);

  if (models.includes(preferred)) {
    return preferred;
  }

  if (preferred === "zen/minimax2.5-free" && models.includes("opencode/minimax-m2.5-free")) {
    console.log("[task-loop] model fallback: zen/minimax2.5-free -> opencode/minimax-m2.5-free");
    return "opencode/minimax-m2.5-free";
  }

  return preferred;
}

function shellCommandAllowed(cmd) {
  const normalized = cmd.trim();
  return (
    normalized.startsWith("./scripts/") ||
    normalized.startsWith("scripts/") ||
    normalized.startsWith("bash scripts/") ||
    normalized.startsWith("node scripts/")
  );
}

function executeTask(task, model) {
  if (task.kind === "shell") {
    const cmd = task.command;
    if (!cmd) {
      throw new Error(`Task '${task.id}' has kind=shell but no command defined`);
    }
    if (!shellCommandAllowed(cmd)) {
      throw new Error(`Rejected unsafe shell command: ${cmd}`);
    }
    if (SHELL_METACHARACTERS.test(cmd)) {
      throw new Error(`Rejected shell command containing unsafe metacharacters: ${cmd}`);
    }
    const tokens = cmd.trim().split(/\s+/);
    const result = runCommand(tokens[0], tokens.slice(1), REPO_ROOT);
    if (result.code !== 0) {
      throw new Error(`Shell task failed: ${result.stderr || result.stdout}`);
    }
    return result.stdout.trim();
  }

  if (task.kind === "opencode") {
    const instruction = task.instruction;
    if (!instruction) {
      throw new Error(`Task '${task.id}' has kind=opencode but no instruction defined`);
    }
    const result = runCommand("opencode", ["run", "-m", model, instruction.trim()], REPO_ROOT);
    if (result.code !== 0) {
      throw new Error(`OpenCode task failed: ${result.stderr || result.stdout}`);
    }
    return result.stdout.trim();
  }

  throw new Error(`Task '${task.id}' has unknown kind: ${task.kind}`);
}

function timestampNow() {
  return new Date().toISOString();
}

function runIteration(args) {
  const tasks = readTasks();
  const state = readState();
  const now = args.at || new Date();

  const due = tasks.filter((task) => {
    if (!task.schedule) return false;
    const taskState = state[task.id] || {};
    return cronMatches(task.schedule, now) && !alreadyRanThisSlot(taskState, now);
  });

  if (due.length === 0) {
    console.log(`[task-loop] ${now.toISOString()} no due tasks`);
    return;
  }

  console.log(`[task-loop] ${now.toISOString()} due tasks: ${due.map((d) => d.id).join(", ")}`);

  let stateChanged = false;

  for (const task of due) {
    console.log(`[task-loop] running: ${task.id}`);
    if (args.dryRun) {
      const detail = task.kind === "shell" ? `command: ${task.command}` : `instruction: ${(task.instruction || "").slice(0, DRY_RUN_INSTRUCTION_PREVIEW_LENGTH).replace(/\n/g, " ")}...`;
      console.log(`[task-loop] dry-run ${task.id} (${task.kind}) — ${detail}`);
      continue;
    }

    try {
      executeTask(task, args.model);
      if (!state[task.id]) state[task.id] = {};
      state[task.id].last_run = timestampNow();
      state[task.id].last_error = null;
      stateChanged = true;
    } catch (err) {
      const message = String(err && err.message ? err.message : err).replace(/\s+/g, " ").slice(0, 200);
      console.error(`[task-loop] failed ${task.id}: ${message}`);
      if (!state[task.id]) state[task.id] = {};
      state[task.id].last_error = `${timestampNow()} — ${message}`;
      stateChanged = true;
    }
  }

  if (stateChanged) {
    writeState(state);
  }
}

function main() {
  const args = parseArgs(process.argv);
  args.model = resolveModel(args.model);

  if (args.once) {
    runIteration(args);
    return;
  }

  console.log(`[task-loop] started poll=${args.pollSeconds}s model=${args.model}`);
  runIteration(args);
  setInterval(() => runIteration(args), args.pollSeconds * 1000);
}

main();

