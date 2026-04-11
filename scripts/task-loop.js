#!/usr/bin/env node
// DEPRECATED: This JavaScript scheduler has been superseded by the Go scheduler
// in scheduler/main.go.  Use `go run ./scheduler` (or the compiled binary) instead.
// This file is kept temporarily for reference and will be removed in a future commit.

const fs = require("fs");
const path = require("path");
const { spawnSync } = require("child_process");

const REPO_ROOT = path.resolve(__dirname, "..");
const TASKS_FILE = path.join(REPO_ROOT, "crons", "tasks.md");
const DEFAULT_MODEL = process.env.OPENCODE_TASK_MODEL || "zen/minimax2.5-free";
const DEFAULT_POLL_SECONDS = Number(process.env.TASK_LOOP_POLL_SECONDS || 60);

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
  console.log(`Usage: node scripts/task-loop.js [options]\n\nOptions:\n  --once                 Run one scheduler iteration and exit\n  --dry-run              Do not execute tasks or modify files\n  --poll-seconds N       Loop interval in seconds (default: ${DEFAULT_POLL_SECONDS})\n  --model provider/model OpenCode model for task compilation\n  --at ISO_TIME          Simulate time for one iteration (testing)\n  -h, --help             Show help`);
}

function readTasksFile() {
  if (!fs.existsSync(TASKS_FILE)) {
    throw new Error(`Tasks file not found: ${TASKS_FILE}`);
  }
  return fs.readFileSync(TASKS_FILE, "utf8");
}

function splitTaskSections(content) {
  const sections = [];
  const regex = /^##\s+(.+)$/gm;
  let match;
  const starts = [];
  while ((match = regex.exec(content)) !== null) {
    starts.push({
      title: match[1].trim(),
      index: match.index,
    });
  }

  for (let i = 0; i < starts.length; i++) {
    const start = starts[i].index;
    const end = i + 1 < starts.length ? starts[i + 1].index : content.length;
    sections.push({
      title: starts[i].title,
      start,
      end,
      text: content.slice(start, end),
    });
  }

  return sections;
}

function parseTaskSection(section) {
  const scheduleMatch = section.text.match(/\*\*Schedule:\*\*\s*`([^`]+)`/);
  const actionMatch = section.text.match(/\*\*Action:\*\*\s*([\s\S]*?)\n-\s+\*\*Last run:\*\*/);
  const lastRunMatch = section.text.match(/-\s+\*\*Last run:\*\*\s*(.+)/);

  if (!scheduleMatch || !actionMatch || !lastRunMatch) {
    return null;
  }

  const lastErrorMatch = section.text.match(/-\s+\*\*Last error:\*\*\s*(.+)/);

  return {
    name: section.title,
    schedule: scheduleMatch[1].trim(),
    action: actionMatch[1].trim(),
    lastRunRaw: lastRunMatch[1].trim(),
    lastErrorRaw: lastErrorMatch ? lastErrorMatch[1].trim() : null,
    section,
  };
}

function parseLastRun(raw) {
  if (!raw || raw.includes("(never)")) return null;
  const parsed = new Date(raw.replace(/_/g, "").trim());
  if (Number.isNaN(parsed.getTime())) return null;
  return parsed;
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

function alreadyRanThisSlot(task, now) {
  const lastRun = parseLastRun(task.lastRunRaw);
  if (!lastRun) return false;
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

function compileTaskWithLLM(task, model) {
  const prompt = [
    "Convert this scheduled task action into an executable task instruction.",
    "Return ONLY valid JSON with this exact schema:",
    '{"type":"opencode"|"shell","payload":"...","reason":"..."}',
    "Rules:",
    "- Prefer type=opencode for tasks requiring reasoning, summarization, or file edits.",
    "- Use type=shell only for deterministic script commands.",
    "- payload must be plain text without markdown fences.",
    "Task:",
    `name: ${task.name}`,
    `schedule: ${task.schedule}`,
    `action: ${task.action}`,
    `repo: ${REPO_ROOT}`,
  ].join("\n");

  const result = runCommand("opencode", ["run", "-m", model, prompt], REPO_ROOT);
  if (result.code !== 0) {
    throw new Error(`opencode task compile failed: ${result.stderr || result.stdout}`);
  }

  const extracted = extractJsonObject(result.stdout);
  if (!extracted) {
    throw new Error(`Could not parse JSON from compiler output: ${result.stdout.trim()}`);
  }

  const parsed = JSON.parse(extracted);
  if (!parsed || (parsed.type !== "opencode" && parsed.type !== "shell") || typeof parsed.payload !== "string") {
    throw new Error(`Invalid compiler JSON: ${extracted}`);
  }
  return parsed;
}

function extractJsonObject(text) {
  const first = text.indexOf("{");
  const last = text.lastIndexOf("}");
  if (first < 0 || last < first) return null;
  return text.slice(first, last + 1).trim();
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

function executeCompiledTask(compiled, model) {
  if (compiled.type === "shell") {
    if (!shellCommandAllowed(compiled.payload)) {
      throw new Error(`Rejected unsafe shell payload: ${compiled.payload}`);
    }
    const result = runCommand("bash", ["-lc", compiled.payload], REPO_ROOT);
    if (result.code !== 0) {
      throw new Error(`Shell task failed: ${result.stderr || result.stdout}`);
    }
    return result.stdout.trim();
  }

  const result = runCommand("opencode", ["run", "-m", model, compiled.payload], REPO_ROOT);
  if (result.code !== 0) {
    throw new Error(`OpenCode task failed: ${result.stderr || result.stdout}`);
  }
  return result.stdout.trim();
}

function updateTaskMetadata(content, taskName, updates) {
  const sections = splitTaskSections(content);
  const target = sections.find((s) => s.title === taskName);
  if (!target) return content;

  let updatedSection = target.text;

  if (updates.lastRun) {
    updatedSection = updatedSection.replace(/(-\s+\*\*Last run:\*\*\s*)(.+)/, `$1${updates.lastRun}`);
  }

  if (updates.lastError) {
    if (/-\s+\*\*Last error:\*\*/.test(updatedSection)) {
      updatedSection = updatedSection.replace(/(-\s+\*\*Last error:\*\*\s*)(.+)/, `$1${updates.lastError}`);
    } else {
      updatedSection = updatedSection.replace(/(-\s+\*\*Last run:\*\*\s*.+)/, `$1\n- **Last error:** ${updates.lastError}`);
    }
  }

  return content.slice(0, target.start) + updatedSection + content.slice(target.end);
}

function timestampNow() {
  return new Date().toISOString();
}

function runIteration(args) {
  let content = readTasksFile();
  const sections = splitTaskSections(content)
    .map(parseTaskSection)
    .filter(Boolean);

  const now = args.at || new Date();
  const due = sections.filter((task) => cronMatches(task.schedule, now) && !alreadyRanThisSlot(task, now));

  if (due.length === 0) {
    console.log(`[task-loop] ${now.toISOString()} no due tasks`);
    return;
  }

  console.log(`[task-loop] ${now.toISOString()} due tasks: ${due.map((d) => d.name).join(", ")}`);

  for (const task of due) {
    console.log(`[task-loop] running: ${task.name}`);
    if (args.dryRun) {
      console.log(`[task-loop] dry-run action: ${task.action}`);
      continue;
    }

    try {
      const compiled = compileTaskWithLLM(task, args.model);
      console.log(`[task-loop] compiled ${task.name}: ${compiled.type} (${compiled.reason || "no reason"})`);
      executeCompiledTask(compiled, args.model);
      content = updateTaskMetadata(content, task.name, {
        lastRun: timestampNow(),
      });
    } catch (err) {
      const message = String(err && err.message ? err.message : err).replace(/\s+/g, " ").slice(0, 200);
      console.error(`[task-loop] failed ${task.name}: ${message}`);
      content = updateTaskMetadata(content, task.name, {
        lastError: `${timestampNow()} — ${message}`,
      });
    }
  }

  fs.writeFileSync(TASKS_FILE, content);
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
