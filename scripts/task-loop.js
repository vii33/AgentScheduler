#!/usr/bin/env node

const fs = require("fs");
const path = require("path");
const { spawnSync } = require("child_process");
const yaml = require("js-yaml");

const REPO_ROOT = path.resolve(__dirname, "..");
const TASKS_FILE = path.join(REPO_ROOT, "crons", "tasks.yaml");
const MINICLAW_DIR = path.join(REPO_ROOT, ".miniclaw");
const STATE_FILE = path.join(MINICLAW_DIR, "task-state.json");
const LOCK_FILE = path.join(MINICLAW_DIR, "task-loop.lock");
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
  console.log(`Usage: node scripts/task-loop.js [options]\n\nOptions:\n  --once                 Run one scheduler iteration and exit\n  --dry-run              Do not execute tasks or modify files\n  --poll-seconds N       Loop interval in seconds (default: ${DEFAULT_POLL_SECONDS})\n  --model provider/model OpenCode model for opencode tasks\n  --at ISO_TIME          Simulate time for one iteration (testing)\n  -h, --help             Show help`);
}

function readTasks() {
  if (!fs.existsSync(TASKS_FILE)) {
    throw new Error(`Tasks file not found: ${TASKS_FILE}`);
  }
  const content = fs.readFileSync(TASKS_FILE, "utf8");
  const tasks = parseTasksYaml(content).filter((task) => task.enabled !== false);
  tasks.forEach((task, index) => validateTask(task, index));
  return tasks;
}

function parseTasksYaml(content) {
  const lines = content.split(/\r?\n/);
  const tasks = [];
  let current = null;
  let foundTasksKey = false;

  for (let i = 0; i < lines.length; i++) {
    const rawLine = lines[i];
    const line = rawLine.replace(/\t/g, "    ");
    const trimmed = line.trim();

    if (!trimmed || trimmed.startsWith("#")) {
      continue;
    }

    if (trimmed === "tasks:") {
      foundTasksKey = true;
      continue;
    }

    const itemMatch = line.match(/^  -\s+([A-Za-z_][A-Za-z0-9_-]*):\s*(.+?)\s*$/);
    if (itemMatch) {
      if (current) {
        tasks.push(current);
      }
      current = {};
      current[itemMatch[1]] = parseYamlScalar(itemMatch[2]);
      continue;
    }

    const fieldMatch = line.match(/^    ([A-Za-z_][A-Za-z0-9_-]*):\s*(.*?)\s*$/);
    if (fieldMatch) {
      if (!current) {
        throw new Error(`Invalid tasks file: field without task entry: ${rawLine}`);
      }
      if (fieldMatch[2] === "|" || fieldMatch[2] === ">") {
        const blockLines = [];
        const fold = fieldMatch[2] === ">";
        while (i + 1 < lines.length) {
          const nextRawLine = lines[i + 1];
          if (!/^      /.test(nextRawLine) && nextRawLine.trim() !== "") {
            break;
          }
          i += 1;
          blockLines.push(nextRawLine.trim() === "" ? "" : nextRawLine.slice(6));
        }
        current[fieldMatch[1]] = fold
          ? blockLines.join(" ").replace(/\s+/g, " ").trim()
          : blockLines.join("\n").trim();
      } else {
        current[fieldMatch[1]] = parseYamlScalar(fieldMatch[2]);
      }
      continue;
    }

    throw new Error(`Invalid tasks file: unsupported line: ${rawLine}`);
  }

  if (current) {
    tasks.push(current);
  }

  if (!foundTasksKey || tasks.length === 0) {
    throw new Error("Invalid tasks file: expected a top-level 'tasks' list");
  }

  return tasks;
}

function parseYamlScalar(value) {
  const trimmed = value.trim();

  if (trimmed === "true") return true;
  if (trimmed === "false") return false;
  if (trimmed === "null") return null;

  if (trimmed.startsWith('"') && trimmed.endsWith('"')) {
    try {
      return JSON.parse(trimmed);
    } catch (err) {
      throw new Error(`Invalid quoted YAML string: ${trimmed}`);
    }
  }

  if (trimmed.startsWith("'") && trimmed.endsWith("'")) {
    return trimmed.slice(1, -1);
  }

  return trimmed;
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

function ensureMiniclawDir() {
  if (!fs.existsSync(MINICLAW_DIR)) {
    fs.mkdirSync(MINICLAW_DIR, { recursive: true });
  }
}

function writeState(state) {
  ensureMiniclawDir();
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

function readLock() {
  try {
    return JSON.parse(fs.readFileSync(LOCK_FILE, "utf8"));
  } catch (err) {
    return null;
  }
}

function processIsLive(pid) {
  if (!Number.isInteger(pid) || pid <= 0) {
    return false;
  }

  try {
    process.kill(pid, 0);
    return true;
  } catch (err) {
    return err && err.code === "EPERM";
  }
}

function describeLock(lock) {
  if (!lock || !lock.pid) {
    return "unknown owner";
  }
  return `pid=${lock.pid}${lock.started_at ? ` started_at=${lock.started_at}` : ""}`;
}

function acquireSchedulerLock() {
  ensureMiniclawDir();
  const lock = {
    pid: process.pid,
    started_at: new Date().toISOString(),
  };
  const content = JSON.stringify(lock, null, 2) + "\n";

  for (let attempt = 0; attempt < 2; attempt++) {
    try {
      const fd = fs.openSync(LOCK_FILE, "wx");
      try {
        fs.writeFileSync(fd, content);
      } finally {
        fs.closeSync(fd);
      }
      return lock;
    } catch (err) {
      if (!err || err.code !== "EEXIST") {
        throw err;
      }

      const existingLock = readLock();
      if (processIsLive(existingLock && existingLock.pid)) {
        throw new Error(`Refusing to start: live scheduler already owns ${LOCK_FILE} (${describeLock(existingLock)})`);
      }

      console.warn(`[task-loop] removing stale lock: ${LOCK_FILE} (${describeLock(existingLock)})`);
      fs.unlinkSync(LOCK_FILE);
    }
  }

  throw new Error(`Could not acquire scheduler lock: ${LOCK_FILE}`);
}

function releaseSchedulerLock(lock) {
  const currentLock = readLock();
  if (!currentLock || currentLock.pid !== lock.pid || currentLock.started_at !== lock.started_at) {
    return;
  }

  try {
    fs.unlinkSync(LOCK_FILE);
  } catch (err) {
    if (!err || err.code !== "ENOENT") {
      console.warn(`[task-loop] Warning: could not remove lock ${LOCK_FILE}: ${err.message}`);
    }
  }
}

function registerLockCleanup(lock) {
  let released = false;
  function cleanup() {
    if (released) return;
    released = true;
    releaseSchedulerLock(lock);
  }

  process.on("exit", cleanup);
  for (const signal of ["SIGINT", "SIGTERM", "SIGHUP"]) {
    process.once(signal, () => {
      cleanup();
      process.exit(128 + (signal === "SIGHUP" ? 1 : signal === "SIGINT" ? 2 : 15));
    });
  }
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

function formatIsoDate(date) {
  return date.toISOString().slice(0, 10);
}

function formatIsoWeek(date) {
  const d = new Date(Date.UTC(date.getUTCFullYear(), date.getUTCMonth(), date.getUTCDate()));
  const day = d.getUTCDay() || 7;
  d.setUTCDate(d.getUTCDate() + 4 - day);
  const yearStart = new Date(Date.UTC(d.getUTCFullYear(), 0, 1));
  const week = Math.ceil((((d - yearStart) / 86400000) + 1) / 7);
  return `${d.getUTCFullYear()}-W${String(week).padStart(2, "0")}`;
}

function applyTaskPlaceholders(text, now) {
  return text
    .replaceAll("YYYY-MM-DD", formatIsoDate(now))
    .replaceAll("YYYY-Www", formatIsoWeek(now));
}

function splitShellWords(command) {
  const args = [];
  let current = "";
  let quote = null;
  let escaping = false;

  for (const char of command) {
    if (escaping) {
      current += char;
      escaping = false;
      continue;
    }

    if (char === "\\") {
      escaping = true;
      continue;
    }

    if (quote) {
      if (char === quote) {
        quote = null;
      } else {
        current += char;
      }
      continue;
    }

    if (char === '"' || char === "'") {
      quote = char;
      continue;
    }

    if (/\s/.test(char)) {
      if (current) {
        args.push(current);
        current = "";
      }
      continue;
    }

    current += char;
  }

  if (escaping) {
    throw new Error(`Rejected shell command with trailing escape: ${command}`);
  }

  if (quote) {
    throw new Error(`Rejected shell command with unterminated quote: ${command}`);
  }

  if (current) {
    args.push(current);
  }

  return args;
}

function executeTask(task, model, now) {
  if (task.kind === "shell") {
    const cmd = applyTaskPlaceholders(task.command, now);
    if (!cmd) {
      throw new Error(`Task '${task.id}' has kind=shell but no command defined`);
    }
    if (!shellCommandAllowed(cmd)) {
      throw new Error(`Rejected unsafe shell command: ${cmd}`);
    }
    const tokens = splitShellWords(cmd);
    const result = runCommand(tokens[0], tokens.slice(1), REPO_ROOT);
    if (result.code !== 0) {
      throw new Error(`Shell task failed: ${result.stderr || result.stdout}`);
    }
    return result.stdout.trim();
  }

  if (task.kind === "opencode") {
    const instruction = applyTaskPlaceholders(task.instruction, now);
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
    const taskState = state[task.id] || {};
    return cronMatches(task.schedule, now) && !alreadyRanThisSlot(taskState, now);
  });

  if (due.length === 0) {
    console.log(`[task-loop] ${now.toISOString()} no due tasks`);
    return;
  }

  console.log(`[task-loop] ${now.toISOString()} due tasks: ${due.map((task) => task.id).join(", ")}`);

  let resolvedModel = null;
  function getModel() {
    if (resolvedModel === null) {
      resolvedModel = resolveModel(args.model);
    }
    return resolvedModel;
  }

  let stateChanged = false;

  for (const task of due) {
    console.log(`[task-loop] running: ${task.id}`);
    if (args.dryRun) {
      const renderedText = task.kind === "shell"
        ? applyTaskPlaceholders(task.command || "", now)
        : applyTaskPlaceholders(task.instruction || "", now);
      const detail = task.kind === "shell"
        ? `command: ${renderedText}`
        : (() => {
            const preview = renderedText.slice(0, DRY_RUN_INSTRUCTION_PREVIEW_LENGTH).replace(/\n/g, " ");
            const suffix = renderedText.length > DRY_RUN_INSTRUCTION_PREVIEW_LENGTH ? "..." : "";
            return `instruction: ${preview}${suffix}`;
          })();
      console.log(`[task-loop] dry-run ${task.id} (${task.kind}) — ${detail}`);
      continue;
    }

    try {
      executeTask(task, task.kind === "opencode" ? getModel() : args.model, now);
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

  if (args.once) {
    runIteration(args);
    return;
  }

  const lock = acquireSchedulerLock();
  registerLockCleanup(lock);

  console.log(`[task-loop] started poll=${args.pollSeconds}s model=${args.model} lock=${LOCK_FILE}`);
  runIteration(args);
  setInterval(() => runIteration(args), args.pollSeconds * 1000);
}

try {
  main();
} catch (err) {
  console.error(`[task-loop] ${err && err.message ? err.message : err}`);
  process.exit(1);
}

