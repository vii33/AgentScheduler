# Research: File-based Agent Memory, Dreaming, Consolidation, and Conflict Resolution

_Last researched: 2026-08-21_

This document consolidates research into open-source and published agent-memory systems relevant to AgentScheduler and an OpenCode/Obsidian-style workflow. It is a research and design reference, not an implementation specification.

The target environment assumed here is:

- Markdown/Obsidian as the source of truth
- existing Daily, project, and knowledge folders
- frontmatter such as `last_updated` and `summary`
- lexical retrieval through `ripgrep`
- ranked retrieval through QMD/BM25
- scheduled agent jobs through AgentScheduler

The core conclusion is that this stack already solves most of the storage problem. The missing layer is a disciplined **memory lifecycle**: extraction, reflection, consolidation, promotion, supersession, archival, provenance, and conflict resolution.

---

## Executive summary

Across OpenAI Codex, OpenClaw, llm-wiki-memory, Hindsight, Basic Memory, Letta/MemGPT-inspired designs, and research systems such as Generative Agents, MemoryBank, A-MEM, and Sleep-time Compute, the strongest common architecture is:

```text
Chat / work session
        |
        v
Session extraction
        |
        v
Episodic memory
(daily notes / session summaries)
        |
        v
Reflection / dreaming
        |
        +-- deduplicate
        +-- cluster
        +-- identify recurring patterns
        +-- detect contradictions
        +-- collect provenance
        |
        v
Consolidation
        |
        +-- add
        +-- merge
        +-- update
        +-- supersede
        +-- archive
        +-- ignore
        |
        v
Durable knowledge
(project/wiki/core memory)
        |
        v
Retrieval
(BM25 / rg / metadata / optional semantic search)
```

The most important design principles are:

1. **Do not treat memory as chat-history search.** Keep episodic evidence separate from durable knowledge.
2. **Do not promote every session.** A no-op is a valid and often desirable result.
3. **Promotion and retrieval are different problems.** A memory can be easy to retrieve without deserving permanent promotion.
4. **Usage is evidence of value.** Repeated successful recall is a strong promotion signal.
5. **Recency alone must never resolve contradictions.** Evidence strength and explicit user correction matter more.
6. **Never hard-delete automatically.** Prefer supersession or reversible archival.
7. **Provenance is part of the memory.** Durable claims should remain traceable to supporting evidence.
8. **Curated knowledge should not decay merely because it is old.** Recency decay belongs primarily on episodic memory and retrieval ranking.
9. **Dreaming/reflection should be offline.** Expensive synthesis belongs between sessions, not in the critical request path.
10. **Markdown can remain canonical.** SQLite, embeddings, or indexes are acceptable as disposable derived state.

For AgentScheduler, the most promising architecture is therefore not a new memory database. It is a scheduled **Dreaming Agent over Markdown**.

---

# 1. Existing AgentScheduler architecture

AgentScheduler already has a useful layered memory workflow:

- `memory/history/YYYY-MM-DD.md`: raw daily session export
- `MEMORY.md`: small synthesized durable memory
- `memory/knowledge/`: longer-form weekly or topical knowledge

The current workflow is documented in [`docs/memory-workflow.md`](memory-workflow.md). It already has scheduled export, daily analysis, and weekly review.

Its current limitation is also documented there: long-term memory quality is largely enforced by prompt discipline. There is no hard size budget, automatic deduplication, formal confidence/evidence model, retrieval-usage telemetry, or systematic conflict/supersession process.

This research is therefore guidance for evolving the existing pipeline rather than replacing it.

---

# 2. OpenAI Codex memory pipeline

OpenAI's Codex repository contains public memory-writing prompts and implementation documentation. This is especially valuable because it exposes both the **per-session extraction prompt** and the **global consolidation prompt**.

Primary sources:

- [Codex memories README](https://github.com/openai/codex/blob/main/codex-rs/memories/README.md)
- [Stage-one extraction prompt](https://github.com/openai/codex/blob/main/codex-rs/memories/write/templates/memories/stage_one_system.md)
- [Phase-two consolidation prompt](https://github.com/openai/codex/blob/main/codex-rs/memories/write/templates/memories/consolidation.md)

## 2.1 Two-phase design

Codex separates memory creation into two phases.

### Phase 1: rollout extraction

Each eligible rollout is analyzed independently and produces structured memory artifacts such as:

- a rollout summary
- a rollout slug
- raw memory content

The critical idea is the **minimum-signal gate**.

The stage-one prompt asks, in effect:

> Will a future agent plausibly act better because of what I write here?

If not, the correct result is no memory output.

Examples of low-signal material called out by the prompt include:

- one-off random queries
- generic status updates without takeaways
- temporary/live facts that should be re-queried
- obvious or common knowledge
- work with no reusable learning
- material with no durable preference or constraint

This is an important design principle for AgentScheduler: **memory generation should optimize for future behavioral improvement, not transcript compression**.

### Phase 2: global consolidation

Phase 2 consolidates selected phase-one results into durable filesystem artifacts such as:

```text
memory_summary.md
MEMORY.md
raw_memories.md
skills/
rollout_summaries/
```

The consolidation process is incremental and may make no content changes if nothing meaningful has changed.

## 2.2 What Codex considers high-signal memory

The public consolidation prompt prioritizes information that helps future agents:

- understand stable user operating preferences
- recognize recurring dislikes or correction patterns
- avoid known failure modes
- reuse proven workflows and verification checklists
- find repository/task truth faster
- save tool calls and reasoning effort
- retain difficult-to-discover shortcuts

It explicitly deprioritizes generic advice and exploratory discussion.

A particularly useful rule is that user-preference evidence should remain auditable and source-faithful rather than being over-generalized into vague abstractions.

A useful memory is therefore not merely:

```text
User likes evidence-backed debugging.
```

but something closer to:

```text
When debugging this class of problem, the user corrected the agent to inspect the actual config path before answering.
Implication: verify the concrete routing/config chain before concluding.
```

This is highly relevant to a Markdown wiki because it argues for retaining enough evidence and wording to make durable guidance inspectable.

## 2.3 Usage and recency in Codex phase-two selection

The Codex memories README documents that phase-two selection does not simply process everything forever.

Eligible stage-one memories are selected using usage information:

- memories outside a configured `max_unused_days` window can be ignored
- if `last_usage` is absent, generation time is used as fallback
- eligible memories are ranked primarily by `usage_count`, then recent usage/generation time

This is a second independent signal beyond the LLM's original judgment: **memories that are actually used get priority for consolidation**.

## 2.4 Contradictions and stale guidance

The phase-two prompt instructs the agent to preserve still-supported guidance, remove stale references, and rewrite or split blocks only when needed. It stresses evidence-based updates, freshness via `updated_at`, and deeper inspection of rollout evidence when overlap, ambiguity, conflict, or staleness must be resolved.

The useful derived rule is:

```text
newer != automatically truer
```

A better conflict decision should consider:

1. explicit user correction
2. current verified evidence
3. repeated evidence across sessions
4. recency
5. older validated evidence
6. inference/speculation

The exact ordering above is a design synthesis rather than a literal Codex scoring formula, but it follows the prompt's evidence, validation, user-preference, recurrence, and `updated_at` guidance.

## 2.5 Key Codex lessons for AgentScheduler

Adopt:

- a no-op / minimum-signal gate
- separate extraction and consolidation jobs
- immutable raw evidence
- incremental consolidation
- source-faithful preference evidence
- usage as a promotion/selection signal
- dense routing summaries separate from detailed knowledge

Avoid:

- summarizing every session into durable memory
- rewriting all memory from scratch each night
- collapsing specific user feedback into abstract personality labels

---

# 3. OpenClaw: dreaming, promotion, retrieval, and provenance

OpenClaw provides one of the clearest public architectures for background memory consolidation.

Primary sources:

- [Dreaming](https://github.com/openclaw/openclaw/blob/main/docs/concepts/dreaming.md)
- [Memory architecture](https://github.com/openclaw/openclaw/blob/main/docs/concepts/memory-architecture.md)
- [Memory search](https://github.com/openclaw/openclaw/blob/main/docs/concepts/memory-search.md)
- [Memory CLI](https://github.com/openclaw/openclaw/blob/main/docs/cli/memory.md)
- [Memory configuration](https://github.com/openclaw/openclaw/blob/main/docs/reference/memory-config.md)

## 3.1 Memory tiers

OpenClaw explicitly separates tiers:

| Tier | Example surface | Purpose |
|---|---|---|
| Instructions | `AGENTS.md` | human-owned instructions |
| Curated core | `MEMORY.md`, `USER.md` | small durable always-loaded knowledge |
| Episodic | dated memory files, transcripts | large searchable evidence |
| Prospective | intents / scheduled tasks | future-triggered behavior |
| Review | `DREAMS.md`, phase reports | human-readable consolidation audit trail |

The key boundary is between episodic and curated memory. Episodic data does **not** automatically become durable core memory.

## 3.2 Dreaming phases

OpenClaw's dreaming cycle runs:

```text
Light -> REM -> Deep
```

### Light

Light sleep stages recent short-term material:

- reads recent traces/daily memory/transcripts
- deduplicates signals
- forms candidates
- records reinforcement signals
- does not write long-term memory

### REM

REM performs reflection:

- identifies themes
- surfaces recurring ideas
- creates reflection summaries
- records reinforcement signals
- still does not write durable memory

### Deep

Deep is the phase that promotes durable information.

It:

- ranks candidates
- applies deterministic threshold gates
- rehydrates source snippets from live files
- sends qualified candidates plus current durable memory to a consolidation subagent
- validates the rewrite before accepting it

This maps naturally to scheduled tasks:

```text
extract/stage -> reflect -> consolidate
```

and avoids letting a reflection model mutate the source of truth prematurely.

## 3.3 OpenClaw deep promotion score

The current OpenClaw dreaming documentation lists six weighted base signals:

| Signal | Weight |
|---|---:|
| Relevance | 0.30 |
| Frequency | 0.24 |
| Query diversity | 0.15 |
| Recency | 0.15 |
| Consolidation / multi-day recurrence | 0.10 |
| Conceptual richness | 0.06 |

Light/REM reinforcement can additionally contribute small decayed boosts.

Current default deep gates documented in the CLI/config are approximately:

```text
minScore = 0.75
minRecallCount = 3
minUniqueQueries = 3
```

The key conceptual insight is more important than the exact constants:

> A memory graduates because it repeatedly proved useful, not merely because an LLM sounded confident when writing it.

This is arguably the strongest idea to adopt for a BM25-based memory stack.

## 3.4 Retrieval ranking is not promotion ranking

OpenClaw separates normal recall scoring from deep promotion scoring.

Its regular `memory_search` combines vector search and BM25, then applies deterministic ranking roughly as:

```text
hybrid relevance x recency decay x importance multiplier
```

Dated daily notes currently use a **30-day half-life** in recall, while curated files such as `MEMORY.md` and `USER.md` are evergreen.

The dreaming deep-phase configuration uses a separate, shorter **14-day recency half-life default** for promotion scoring and a bounded age window. These are distinct mechanisms and should not be conflated.

This distinction is directly relevant to AgentScheduler:

- retrieval decay controls what appears near the top of search results
- promotion scoring controls what deserves durable consolidation

A memory may decay in retrieval priority without losing its historical content.

## 3.5 MMR and diversity

OpenClaw optionally uses Maximal Marginal Relevance (MMR) after search ranking to reduce redundant results.

The practical lesson is simple: returning five near-duplicate memories is often worse than returning three complementary ones.

For a simple BM25-first stack, MMR is optional rather than foundational. The more important point is to keep ranking and diversity as separate steps.

## 3.6 Provenance and memory poisoning

OpenClaw treats provenance as a structural security property, not merely text metadata.

Its architecture distinguishes trusted owner/agent-derived material from untrusted/system-origin content and prevents untrusted memories from being automatically promoted into the curated core.

For AgentScheduler, even a simpler Markdown implementation should preserve at least:

```yaml
memory:
  sources:
    - Daily/2026-08-20.md
  origin: user|agent|external
```

An external webpage summarized by the agent should never silently become an always-loaded instruction merely because it was retrieved frequently.

## 3.7 Safe rewrite validation

OpenClaw stores the pre-image of accepted `MEMORY.md` rewrites and applies rewrite validation. Its memory config documents a default maximum prior-entry-loss fraction of `0.25`.

This is a useful safety pattern:

- snapshot the previous curated state
- reject suspiciously destructive rewrites
- record added/merged/superseded changes
- make the consolidation result human-reviewable

For Git-backed Markdown, Git already supplies much of the rollback mechanism, but a Dreaming report remains valuable as an audit summary.

---

# 4. llm-wiki-memory: Markdown-first capture, compile, and consolidation

`ctxr-dev/llm-wiki-memory` is one of the closest open-source matches to an Obsidian-based design.

Primary source:

- [llm-wiki-memory repository and documentation](https://github.com/ctxr-dev/llm-wiki-memory)

The project keeps memory as local, Git-versioned Markdown and avoids requiring a cloud-backed database as the source of truth.

## 4.1 Storage model

The wiki is divided into categories such as:

```text
daily/
knowledge/
self_improvement/
plans/
investigations/
```

The workflow distinguishes:

```text
session capture -> daily notes -> compile -> durable knowledge / lessons
```

This fits naturally with an Obsidian vault in which daily and project notes already exist.

## 4.2 Compile vs consolidate

A useful distinction is that **compile** and **consolidate** are separate operations.

Compile promotes/categorizes information from daily notes into durable leaves.

Consolidation maintains existing durable memory through:

- deterministic deduplication
- housekeeping
- stale-note refresh
- optional LLM review

The LLM review can return outcomes such as:

```text
keep
rewrite
archive
fallback
```

Nothing needs to be hard-deleted. Archival is reversible.

## 4.3 Category-specific lifecycle

The wiki layout allows categories to follow different refinement rules. Daily notes, plans, investigation artifacts, and evergreen lessons need not share one global decay/promotion policy.

This maps well to Obsidian frontmatter and directory-specific policy.

## 4.4 Supersession and preservation

A central lesson is to treat stale knowledge as **superseded**, not erased.

Example:

```yaml
status: superseded
superseded_by: Projects/Foo/current-architecture.md
```

This preserves historical truth while keeping default retrieval aligned with current truth.

---

# 5. Hindsight: observations, evidence, and mental models

Hindsight is less suitable as a storage backend for this project because it relies on PostgreSQL/pgvector-style infrastructure, but several of its conceptual layers are highly relevant.

Primary sources:

- [Hindsight repository](https://github.com/vectorize-io/hindsight)
- [Best practices](https://github.com/vectorize-io/hindsight/blob/main/hindsight-docs/src/pages/best-practices.mdx)
- [Mental models](https://github.com/vectorize-io/hindsight/blob/main/hindsight-docs/src/pages/developer/api/mental-models.mdx)

## 5.1 Facts -> observations -> mental models

Hindsight distinguishes roughly:

```text
raw facts
    |
    v
observations
    |
    v
mental models
```

Observations are useful because they represent consolidated claims while retaining supporting evidence.

Instead of treating every statement as an independent durable memory, a system can maintain a claim such as:

```yaml
claim: New project work should prefer Nix.
confidence: high
sources:
  - Daily/2026-05-14.md
  - Daily/2026-08-03.md
history:
  - Docker was previously used more often.
```

New evidence can refine this observation rather than blindly replacing it.

## 5.2 Mental models as prepared views

Hindsight mental models are prepared, periodically refreshed views over accumulated knowledge, for example:

```text
Current project architecture
User development preferences
Known recurring CI problems
Deployment procedure
```

In an Obsidian system, project overview pages or wiki notes can already serve this role. A separate database construct is unnecessary.

## 5.3 Conflict resolution lesson

Hindsight's most important contribution here is the idea that a durable conclusion should retain supporting evidence and evolve as evidence accumulates.

This is stronger than either:

```text
latest statement wins
```

or:

```text
store every statement forever and hope retrieval chooses correctly
```

---

# 6. Basic Memory: Markdown graph primitives

Basic Memory is a Markdown-first knowledge system exposed through MCP.

Primary sources:

- [Basic Memory repository](https://github.com/basicmachines-co/basic-memory)
- [Semantic search documentation](https://github.com/basicmachines-co/basic-memory/blob/main/docs/semantic-search.md)

Its main conceptual contribution is a simple representation of:

```text
Entity
 |- Observation
 `- Relation
```

A note may encode facts and relationships such as:

```markdown
- [decision] Authentication uses OIDC.
- [constraint] Gateway validates the JWT.

- depends_on [[API Gateway]]
- supersedes [[Basic Authentication]]
```

For an Obsidian-first system, the useful lesson is structural rather than technological: many memory relationships can remain plain Markdown and wiki links instead of requiring a separate graph database.

---

# 7. Letta / MemGPT: core, recall, and archival memory

Letta/MemGPT popularized a useful separation between:

```text
Core Memory
Recall Memory
Archival Memory
```

Primary source:

- [Letta documentation and concepts](https://docs.letta.com/)

A practical translation for AgentScheduler is:

```text
Core       -> MEMORY.md / small always-loaded context
Episodic   -> daily session/history notes
Semantic   -> project/wiki/knowledge notes
Archive    -> superseded historical knowledge
```

This argues strongly against turning one `MEMORY.md` file into a giant catch-all store.

---

# 8. Generative Agents: relevance, recency, importance, reflection

The Generative Agents paper introduced one of the best-known memory-retrieval formulations for agents.

Primary source:

- [Generative Agents: Interactive Simulacra of Human Behavior](https://arxiv.org/abs/2304.03442)

The system scores memory relevance using three broad factors:

```text
relevance
recency
importance
```

and periodically creates higher-level **reflections** from accumulated observations.

OpenClaw's architecture clearly builds on this family of ideas, but adds more deterministic promotion gates, usage telemetry, and explicit durable-memory boundaries.

---

# 9. MemoryBank: forgetting curves

MemoryBank explores long-term memory using a forgetting mechanism inspired by Ebbinghaus.

Primary source:

- [MemoryBank: Enhancing Large Language Models with Long-Term Memory](https://arxiv.org/abs/2305.10250)

The useful principle is not that old Markdown should be deleted. It is that **retrieval priority can decay with time** unless reinforced.

For episodic memories, a simple exponential half-life model is intuitive:

```text
recency(age) = 0.5 ^ (age_days / half_life_days)
```

The content stays on disk; only its retrieval or promotion weight decays.

---

# 10. A-MEM: Zettelkasten-like memory evolution

A-MEM is particularly relevant to Obsidian because it treats agent memory more like an evolving Zettelkasten.

Primary source:

- [A-MEM: Agentic Memory for LLM Agents](https://arxiv.org/abs/2502.12110)

New memories gain contextual attributes such as:

```text
context
keywords
tags
links
```

and can cause the representation of older memories to evolve.

The key lesson is that consolidation should not merely append new files. It should be allowed to improve links, summaries, status, and interpretation of existing notes while preserving provenance.

---

# 11. Sleep-time Compute and offline dreaming

Sleep-time Compute formalizes the broader idea that expensive preparation can happen before or between user requests rather than on the request-critical path.

Primary source:

- [Sleep-time Compute: Beyond Inference Scaling at Test-time](https://arxiv.org/abs/2504.13171)

For AgentScheduler, this is a natural fit because scheduled background jobs already exist.

Useful tasks between sessions include:

- candidate extraction
- deduplication
- pattern discovery
- evidence aggregation
- contradiction detection
- promotion scoring
- wiki maintenance
- stale-knowledge review

This is the conceptual justification for a nightly/periodic Dreaming task rather than doing all memory reasoning during interactive OpenCode sessions.

---

# 12. Proposed memory layers for an Obsidian/AgentScheduler system

A practical target structure could be:

```text
Vault/
├── Daily/
│   └── episodic evidence
├── Projects/
│   └── project mental models / current truth
├── Knowledge/
│   └── durable semantic knowledge
├── Lessons/
│   └── reusable procedural knowledge
└── Archive/
    └── superseded historical knowledge
```

The exact folders are less important than the separation of responsibilities.

## Layer A: episodic memory

Purpose:

- evidence
- audit trail
- session continuity
- recent context

Properties:

- append-mostly
- searchable
- allowed to decay in ranking
- not always loaded

## Layer B: durable semantic/project memory

Purpose:

- current project truth
- decisions
- stable preferences
- procedures
- lessons

Properties:

- curated
- provenance-aware
- small enough to route efficiently
- not automatically decayed merely because of age

## Layer C: core/hot context

Purpose:

- always-needed context
- current priorities
- critical constraints

Properties:

- very small
- always loaded
- aggressively curated

---

# 13. Proposed Dreaming cycle

The best synthesis of the researched systems is a three-stage process.

## Phase A: Extract

Read newly added or changed daily/session notes.

For each possible memory candidate, apply the Codex-style gate:

```text
Would this plausibly change how a future agent acts?
```

Potential candidate types:

```text
decision
constraint
preference
validated_fact
lesson
failure_pattern
procedure
open_question
project_state
```

Reject by default:

```text
one-off chatter
live information
generic facts
routine actions
assistant speculation
```

The output of this phase should be candidates, not durable writes.

## Phase B: Reflect

Group candidates by:

- subject
- project
- entity
- decision
- tags
- lexical or semantic similarity

For each candidate ask:

```text
Is this genuinely new?
Does it reinforce an existing claim?
Does it contradict existing knowledge?
Does it supersede something?
Is it only another example?
Does repeated evidence justify a more general rule?
```

No durable mutation should happen yet.

## Phase C: Consolidate

For every qualified candidate, the consolidation agent must choose explicitly among:

```text
ADD
UPDATE
MERGE
SUPERSEDE
ARCHIVE
IGNORE
```

This vocabulary is much safer than a simplistic `ADD`/`DELETE` model.

---

# 14. Conflict resolution model

Contradictions should be treated as an evidence problem, not a recency problem.

Example:

```text
Existing: Project uses REST.
New: All new endpoints should use GraphQL.
```

Possible interpretations:

### Complementary

Both are true:

```text
Existing API remains REST.
New API development uses GraphQL.
```

Action: `MERGE` or refine scope.

### Temporal supersession

REST used to be true but is no longer current:

```text
REST -> migrated to GraphQL
```

Action: `SUPERSEDE` while keeping history.

### Unresolved conflict

Evidence is insufficient or equally strong.

Action: retain both and mark the claim as disputed/needs-review rather than pretending certainty.

## 14.1 Evidence priority

A useful default hierarchy is:

```text
explicit user correction
>
current verified source/tool result
>
explicit user statement
>
repeated evidence across independent sessions
>
single validated observation
>
recent unvalidated observation
>
agent inference
>
assistant suggestion/speculation
```

Critical rule:

```text
newer evidence only wins when epistemic strength is comparable
```

This prevents a fresh speculative statement from overwriting an older verified decision.

---

# 15. Supersession instead of deletion

Automatic memory maintenance should preserve history.

Example:

```yaml
---
status: superseded
superseded_by: Projects/Foo/GraphQL Architecture.md
---
```

Default retrieval can exclude `superseded` entries while historical queries can still inspect them.

This is safer than relying on recency alone to hide stale facts.

---

# 16. Minimal frontmatter extension

Existing frontmatter should remain simple. A possible extension is:

```yaml
---
summary: New endpoints use GraphQL
last_updated: 2026-08-21

memory:
  status: active
  confidence: high
  importance: 8
  origin: user
  sources:
    - Daily/2026-08-20.md
    - Daily/2026-08-21.md
  supersedes:
    - Projects/Foo/REST API.md
  last_recalled: 2026-08-21
  recall_count: 7
---
```

Do not turn frontmatter into a huge knowledge-graph schema. Obsidian links and filesystem scope already encode substantial structure.

---

# 17. Retrieval ranking

Given an existing BM25-capable retrieval layer, the first implementation can remain simple.

Start with a lexical candidate set and apply metadata-aware reranking:

```text
retrieval_score =
    lexical_relevance
  * recency_factor
  * importance_factor
  * status_factor
```

Optionally add boosts for:

```text
current project
matching directory
matching file/symbol
exact tags
current repository
```

Coding-agent search benefits unusually strongly from exact lexical matches because filenames, symbols, CLI commands, error strings, and project identifiers matter.

## 17.1 Recency

For episodic notes:

```text
recency = 0.5 ^ (age_days / half_life_days)
```

Suggested policy ranges:

| Memory type | Suggested half-life |
|---|---:|
| Daily/session context | 21-30 days |
| Project state | 60-90 days |
| Bug investigation | 30-90 days |
| Implementation detail | 60-180 days |
| Lessons | 180+ days |
| Architecture decisions | evergreen |
| Stable preferences | evergreen until contradicted |
| Guardrails/constraints | evergreen until superseded |

The point is not the exact number. The point is that **memory type controls lifecycle**.

---

# 18. Recall telemetry as promotion evidence

The most valuable idea to copy from OpenClaw/Codex is to log actual retrieval usage.

Each recall can produce a tiny event such as:

```json
{
  "path": "Daily/2026-08-12.md",
  "query": "how is auth deployed",
  "score": 7.4,
  "timestamp": "2026-08-21T15:42:00Z",
  "project": "AgentScheduler"
}
```

The Dreaming job can then derive:

- recall count
- unique-query count
- cross-day recurrence
- average relevance
- project diversity or specificity
- last recalled time

This lets the system distinguish:

```text
LLM thought this was important once
```

from:

```text
future agents repeatedly needed this information
```

The second is much stronger evidence for durable promotion.

---

# 19. Promotion score

A practical first version could adapt OpenClaw's ideas while keeping the formula simple:

```text
promotion_score =
    0.30 * recall_relevance
  + 0.25 * recall_frequency
  + 0.15 * query_diversity
  + 0.15 * recurrence_across_days
  + 0.10 * importance
  + 0.05 * recency
```

The exact weights should be treated as tunable policy, not truth.

Add hard qualification rules such as:

```text
recall_count >= 2 or 3
OR explicit durable decision/constraint
OR explicit remember instruction
```

Some durable facts should bypass utility gating. For example:

```text
From now on we deploy only from main.
```

should not have to be recalled three times before it can become durable.

Therefore use two promotion paths:

```text
Explicit durable signal -> immediate consolidation candidate
Observed utility        -> evidence-based consolidation candidate
```

---

# 20. Safe consolidation rules

The consolidator should receive:

- qualified candidates
- existing target note(s)
- source snippets
- timestamps
- provenance/origin
- known supersession links
- relevant retrieval usage

It should be instructed to:

1. make the smallest correct edit
2. preserve still-supported knowledge
3. avoid broad rewrites when a scoped change is enough
4. retain provenance
5. never silently delete contradictory history
6. use `disputed`/`needs_review` if evidence is insufficient
7. prefer `SUPERSEDE` or `ARCHIVE` over hard deletion
8. keep always-loaded memory within a budget
9. produce a short audit report of changes

A Git commit provides rollback, but the Dreaming report should still record what changed and why.

---

# 21. What not to do

## Summarize every session into durable memory

This creates memory pollution. The Codex no-op gate is specifically designed to avoid it.

## Let an LLM assign `importance: 1-10` and stop there

That signal is too unstable by itself. Combine initial importance with observed retrieval utility.

## Let the newest statement always win

A newer statement can be wrong, speculative, or scoped differently.

## Delete the old note when a conflict appears

This destroys provenance and historical truth.

## Put everything in a vector database

Coding/project memory contains exact strings whose lexical identity matters. BM25/FTS and `rg` remain first-class tools.

## Regenerate the whole wiki nightly

Prefer incremental consolidation. Large generative rewrites create accidental information loss and noisy Git history.

---

# 22. Proposed AgentScheduler target architecture

```text
Obsidian / Markdown vault
|
+-- Daily/                episodic evidence
+-- Projects/             current project mental models
+-- Knowledge/            durable semantic knowledge
+-- Lessons/              reusable procedural knowledge
`-- Archive/              superseded history

Retrieval
|
+-- ripgrep               exact lexical search
+-- BM25 / ranked search  candidate generation
`-- metadata filters      scope / status / recency

AgentScheduler Memory Lifecycle
|
+-- session extraction
+-- recall telemetry
+-- light/staging pass
+-- reflection pass
+-- promotion gate
+-- consolidation
+-- contradiction detection
+-- supersession/archive
`-- audit report / Git diff
```

The main architectural stance is:

> **Do not replace Markdown with a memory platform. Add a memory lifecycle over Markdown.**

---

# 23. Concrete requirements for a follow-up implementation agent

A follow-up agent should work from these requirements:

1. Markdown remains the source of truth.
2. Existing Daily/project/wiki files remain canonical evidence and durable knowledge.
3. Retrieval should remain primarily lexical/BM25-friendly; exact code/project strings matter.
4. Session extraction and global consolidation must be separate processes.
5. A session may legitimately produce zero memory candidates.
6. The extraction gate is: **Would this plausibly change how a future agent acts?**
7. Candidate categories should include decisions, constraints, preferences, validated facts, lessons, failure patterns, procedures, open questions, and project state.
8. Dreaming should separate extraction/staging, reflection, and durable consolidation.
9. Consolidation must explicitly support `ADD`, `UPDATE`, `MERGE`, `SUPERSEDE`, `ARCHIVE`, and `IGNORE`.
10. No automatic hard deletion of memory.
11. Every durable claim should retain provenance to source evidence.
12. Conflicts should be resolved using evidence strength, explicit corrections, recurrence, and recency; never recency alone.
13. If conflict remains unresolved, mark it disputed/needs-review rather than fabricating certainty.
14. Retrieval usage should be logged and used as promotion evidence.
15. Repeated recall and query diversity are strong signals that episodic knowledge deserves promotion.
16. Explicit durable decisions/constraints may bypass recall-count gates.
17. Episodic memory may decay in retrieval score.
18. Durable curated knowledge should not decay merely because it is old.
19. Directory/memory type should control decay and consolidation policy.
20. Always-loaded/core memory should have a strict size budget.
21. Project scope, directory, repository, filename, symbol, and tags should be available as ranking boosts.
22. Consolidation should make minimal, incremental edits rather than regenerate whole notes.
23. Existing Git history should be used as rollback/audit support.
24. A human-readable Dreaming report should explain promotions, merges, supersessions, and unresolved conflicts.
25. External/untrusted content must not automatically become durable instructions.

---

# 24. Recommended source systems to copy from

## OpenAI Codex

Best source for:

- public extraction and consolidation prompts
- no-op / minimum-signal filtering
- source-faithful preference evidence
- usage-aware selection
- incremental consolidation

Sources:

- https://github.com/openai/codex/blob/main/codex-rs/memories/README.md
- https://github.com/openai/codex/blob/main/codex-rs/memories/write/templates/memories/stage_one_system.md
- https://github.com/openai/codex/blob/main/codex-rs/memories/write/templates/memories/consolidation.md

## OpenClaw

Best source for:

- Light / REM / Deep dreaming separation
- deterministic promotion gates
- recall-frequency/query-diversity utility signals
- retrieval recency decay
- provenance and trust boundaries
- safe consolidation rewrite validation

Sources:

- https://github.com/openclaw/openclaw/blob/main/docs/concepts/dreaming.md
- https://github.com/openclaw/openclaw/blob/main/docs/concepts/memory-architecture.md
- https://github.com/openclaw/openclaw/blob/main/docs/concepts/memory-search.md
- https://github.com/openclaw/openclaw/blob/main/docs/cli/memory.md
- https://github.com/openclaw/openclaw/blob/main/docs/reference/memory-config.md

## llm-wiki-memory

Best source for:

- Markdown/Git as canonical memory
- Daily -> durable knowledge compilation
- separate compile and consolidate steps
- reversible archive and supersession
- category-specific refinement policies

Source:

- https://github.com/ctxr-dev/llm-wiki-memory

## Hindsight

Best source for:

- evidence-backed observations
- evolving conclusions rather than raw fact replacement
- mental models as prepared current-state views

Sources:

- https://github.com/vectorize-io/hindsight
- https://github.com/vectorize-io/hindsight/blob/main/hindsight-docs/src/pages/best-practices.mdx

## Basic Memory

Best source for:

- Markdown observations and relations
- wiki-style entity graph without requiring a graph database as source of truth

Sources:

- https://github.com/basicmachines-co/basic-memory
- https://github.com/basicmachines-co/basic-memory/blob/main/docs/semantic-search.md

## Research

- Generative Agents — relevance + recency + importance + reflection: https://arxiv.org/abs/2304.03442
- MemoryBank — forgetting/refresh concepts: https://arxiv.org/abs/2305.10250
- A-MEM — evolving Zettelkasten-like agent memory: https://arxiv.org/abs/2502.12110
- Sleep-time Compute — move preparation/reflection off the interactive critical path: https://arxiv.org/abs/2504.13171

---

# 25. Bottom line

AgentScheduler already has the right primitive ingredients: scheduled jobs, raw session exports, durable Markdown, and a small always-loaded memory file.

The next meaningful step is not another storage backend. It is to turn the current compression workflow into a **provenance-aware, usage-aware Dreaming cycle**:

```text
session evidence
    -> extract only high-signal candidates
    -> observe what is actually recalled
    -> reflect across days/sessions
    -> qualify promotion deterministically
    -> consolidate minimally
    -> supersede/archive rather than erase
    -> retain provenance and audit trail
```

This combines the strongest parts of Codex, OpenClaw, llm-wiki-memory, and Hindsight while preserving the transparency and editability of plain Markdown.
