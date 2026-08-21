# Research: File-based Agent Memory, Dreaming, Consolidation, and Conflict Resolution

_Last researched: 2026-08-21_

This document consolidates research into open-source and published agent-memory systems relevant to AgentScheduler and an OpenCode/Obsidian-style workflow. It is a research and design reference, not an implementation specification.

The target environment assumed here is:

- Markdown/Obsidian as the source of truth
- existing Daily, project, and knowledge folders
- frontmatter such as `last_updated` and `summary`
- exact lexical search through `ripgrep`
- some form of BM25/full-text candidate retrieval
- scheduled agent jobs through AgentScheduler

> **Open design question:** QMD is currently available and useful for interactive knowledge search, but it may not be the correct retrieval primitive for the memory subsystem if QMD performs its own internal re-ranking. The architecture proposed here wants control over the final ranker so that BM25, recency, importance, status, scope, provenance, and recall telemetry can be combined deterministically. This may require using a lower-level BM25/FTS library directly instead of taking QMD's final ranking as the base score. See [Section 18](#18-retrieval-architecture-qmd-vs-a-lower-level-bm25-layer).

The core conclusion is that the existing stack already solves most of the storage problem. The missing layer is a disciplined **memory lifecycle**: extraction, reflection, consolidation, promotion, supersession, archival, provenance, conflict resolution, and retrieval telemetry.

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
11. **The memory system should own its final ranking.** If an upstream search tool performs opaque or fixed re-ranking, it may be the wrong primitive for telemetry-driven memory ranking.

For AgentScheduler, the most promising architecture is therefore not a new memory database. It is a scheduled **Dreaming Agent over Markdown**, backed by a retrieval component whose raw scoring remains under our control.

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

# 2. Common memory architecture

The systems researched here differ substantially in implementation, but converge on a small number of layers.

## 2.1 Episodic memory

Episodic memory records what happened:

```text
session transcript
daily note
run output
observed error
conversation correction
```

It is evidence, not necessarily truth.

Properties:

- append-mostly
- large
- searchable
- not always loaded
- allowed to decay in retrieval ranking
- should retain provenance and timestamp

## 2.2 Semantic / durable memory

Durable memory records the best current synthesis:

```text
current architecture
stable user preference
accepted decision
known workflow
reusable lesson
project constraint
```

It is small, curated, and intended to influence future behavior.

## 2.3 Core / hot memory

Some systems additionally keep a tiny always-loaded layer. In AgentScheduler this maps naturally to a deliberately small `MEMORY.md` or routing summary.

## 2.4 Reflection / dreaming

The missing middle is a process that decides how episodic evidence changes durable knowledge:

```text
evidence -> candidates -> reflections -> consolidation -> durable knowledge
```

That process is where most memory quality is won or lost.

---

# 3. OpenAI Codex memory pipeline

OpenAI's Codex repository contains public memory-writing prompts and implementation documentation. This is especially valuable because it exposes both the **per-session extraction prompt** and the **global consolidation prompt**.

Primary sources:

- [Codex memories README](https://github.com/openai/codex/blob/main/codex-rs/memories/README.md)
- [Stage-one extraction prompt](https://github.com/openai/codex/blob/main/codex-rs/memories/write/templates/memories/stage_one_system.md)
- [Phase-two consolidation prompt](https://github.com/openai/codex/blob/main/codex-rs/memories/write/templates/memories/consolidation.md)

## 3.1 Two-phase design

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

## 3.2 What Codex considers high-signal memory

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

## 3.3 Usage and recency in Codex phase-two selection

The Codex memories README documents that phase-two selection does not simply process everything forever.

Eligible stage-one memories are selected using usage information:

- memories outside a configured `max_unused_days` window can be ignored
- if `last_usage` is absent, generation time is used as fallback
- eligible memories are ranked primarily by `usage_count`, then recent usage/generation time

This is a second independent signal beyond the LLM's original judgment: **memories that are actually used get priority for consolidation**.

## 3.4 Contradictions and stale guidance

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

## 3.5 Key Codex lessons for AgentScheduler

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

# 4. OpenClaw: dreaming, promotion, retrieval, and provenance

OpenClaw provides one of the clearest public architectures for background memory consolidation.

Primary sources:

- [Dreaming](https://github.com/openclaw/openclaw/blob/main/docs/concepts/dreaming.md)
- [Memory architecture](https://github.com/openclaw/openclaw/blob/main/docs/concepts/memory-architecture.md)
- [Memory search](https://github.com/openclaw/openclaw/blob/main/docs/concepts/memory-search.md)
- [Memory CLI](https://github.com/openclaw/openclaw/blob/main/docs/cli/memory.md)
- [Memory configuration](https://github.com/openclaw/openclaw/blob/main/docs/reference/memory-config.md)

## 4.1 Memory tiers

OpenClaw explicitly separates tiers:

| Tier | Example surface | Purpose |
|---|---|---|
| Instructions | `AGENTS.md` | human-owned instructions |
| Curated core | `MEMORY.md`, `USER.md` | small durable always-loaded knowledge |
| Episodic | dated memory files, transcripts | large searchable evidence |
| Prospective | intents / scheduled tasks | future-triggered behavior |
| Review | `DREAMS.md`, phase reports | human-readable consolidation audit trail |

The key boundary is between episodic and curated memory. Episodic data does **not** automatically become durable core memory.

## 4.2 Dreaming phases

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

## 4.3 OpenClaw deep promotion score

The OpenClaw dreaming documentation lists six weighted base signals:

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

## 4.4 Retrieval ranking is not promotion ranking

OpenClaw separates normal recall scoring from deep promotion scoring.

Its regular memory search combines lexical/semantic relevance with deterministic adjustments. Dated daily notes use recency decay, while curated files such as `MEMORY.md` and `USER.md` are treated as evergreen.

A relevant implementation detail is that the normal recall layer and Dreaming promotion layer use different recency settings. The important design lesson is not the exact default values, but the separation:

- retrieval decay controls what appears near the top of search results
- promotion scoring controls what deserves durable consolidation

A memory may decay in retrieval priority without losing its historical content.

## 4.5 Diversity

OpenClaw supports result diversification such as MMR so that the final result set is not dominated by near-duplicates.

For AgentScheduler, this should be considered a separate post-ranking concern. First produce a meaningful score, then optionally diversify the top results.

## 4.6 Provenance and memory poisoning

OpenClaw treats provenance as a structural security property, not merely text metadata.

Its architecture distinguishes trusted owner/agent-derived material from untrusted/system-origin content and prevents untrusted memories from automatically becoming trusted curated memory.

For AgentScheduler, even a simpler Markdown implementation should preserve at least:

```yaml
memory:
  sources:
    - Daily/2026-08-20.md
  origin: user|agent|external
```

An external webpage summarized by the agent should never silently become an always-loaded instruction merely because it was retrieved frequently.

## 4.7 Safe rewrite validation

OpenClaw snapshots/reviews durable-memory rewrites and applies validation so that a consolidation pass cannot silently destroy large portions of existing memory.

For Git-backed Markdown, Git already supplies rollback, but a Dreaming report should still record:

- additions
- merges
- supersessions
- archive actions
- unresolved conflicts
- suspiciously destructive proposed rewrites

---

# 5. llm-wiki-memory: Markdown-first capture, compile, and consolidation

`ctxr-dev/llm-wiki-memory` is one of the closest open-source matches to an Obsidian-based design.

Primary source:

- [llm-wiki-memory repository and documentation](https://github.com/ctxr-dev/llm-wiki-memory)

The project keeps memory as local, Git-versioned Markdown and avoids requiring a cloud-backed database as the source of truth.

## 5.1 Storage model

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

## 5.2 Compile vs consolidate

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

## 5.3 Category-specific lifecycle

Daily notes, plans, investigation artifacts, and evergreen lessons do not need to share one global refinement/decay policy.

This maps well to Obsidian frontmatter and directory-specific policy.

## 5.4 Supersession and preservation

A central lesson is to treat stale knowledge as **superseded**, not erased.

Example:

```yaml
status: superseded
superseded_by: Projects/Foo/current-architecture.md
```

This preserves historical truth while keeping default retrieval aligned with current truth.

---

# 6. Hindsight: observations, evidence, and mental models

Hindsight is less suitable as a storage backend for this project because it uses a database-centric stack, but several of its conceptual layers are highly relevant.

Primary sources:

- [Hindsight repository](https://github.com/vectorize-io/hindsight)
- [Hindsight documentation](https://github.com/vectorize-io/hindsight/tree/main/hindsight-docs)

## 6.1 Facts -> observations -> mental models

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

New evidence can refine the observation rather than blindly replacing it.

## 6.2 Mental models as prepared views

Hindsight mental models are prepared, periodically refreshed views over accumulated knowledge, for example:

```text
Current project architecture
User development preferences
Known recurring CI problems
Deployment procedure
```

In an Obsidian system, project overview pages or wiki notes can already serve this role. A separate database construct is unnecessary.

## 6.3 Conflict resolution lesson

The useful conceptual model is:

```text
raw evidence remains immutable
current conclusion evolves
```

This is stronger than either:

```text
latest statement wins
```

or:

```text
store every statement forever and hope retrieval chooses correctly
```

---

# 7. Basic Memory: Markdown graph primitives

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

# 8. Letta / MemGPT: core, recall, and archival memory

Letta/MemGPT popularized a useful separation between:

```text
Core Memory
Recall Memory
Archival Memory
```

Source:

- [Letta documentation](https://docs.letta.com/)

A practical translation for AgentScheduler is:

```text
Core       -> MEMORY.md / small always-loaded context
Episodic   -> daily session/history notes
Semantic   -> project/wiki/knowledge notes
Archive    -> superseded historical knowledge
```

This argues strongly against turning one `MEMORY.md` file into a giant catch-all store.

---

# 9. Generative Agents: relevance, recency, importance, reflection

The Generative Agents paper introduced one of the best-known memory-retrieval formulations for agents.

Source:

- [Generative Agents: Interactive Simulacra of Human Behavior](https://arxiv.org/abs/2304.03442)

The system scores memories using three broad factors:

```text
relevance
recency
importance
```

and periodically creates higher-level **reflections** from accumulated observations.

OpenClaw belongs to this family of ideas but adds deterministic promotion gates, usage telemetry, and stronger boundaries around durable memory.

---

# 10. MemoryBank: forgetting curves

MemoryBank explores long-term memory using a forgetting mechanism inspired by Ebbinghaus.

Source:

- [MemoryBank: Enhancing Large Language Models with Long-Term Memory](https://arxiv.org/abs/2305.10250)

The useful principle is not that old Markdown should be deleted. It is that **retrieval priority can decay with time** unless reinforced.

For episodic memories, a simple exponential half-life model is intuitive:

```text
recency(age) = 0.5 ^ (age_days / half_life_days)
```

The content stays on disk; only its retrieval or promotion weight decays.

---

# 11. A-MEM: Zettelkasten-like memory evolution

A-MEM is particularly relevant to Obsidian because it treats agent memory more like an evolving Zettelkasten.

Source:

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

# 12. Sleep-time Compute and offline dreaming

Sleep-time Compute formalizes the broader idea that expensive preparation can happen before or between user requests rather than on the request-critical path.

Source:

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

# 13. Proposed memory layers for an Obsidian/AgentScheduler system

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

# 14. Proposed Dreaming cycle

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

# 15. Conflict resolution model

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

## 15.1 Evidence priority

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

# 16. Supersession instead of deletion

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

# 17. Minimal frontmatter extension

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

# 18. Retrieval architecture: QMD vs a lower-level BM25 layer

This is an unresolved design decision and should be investigated before implementing the ranker.

## 18.1 Why QMD is attractive

The current Obsidian setup already uses QMD. It provides convenient full-text/ranked search over Markdown and therefore appears, at first glance, to be an ideal retrieval backend.

If the memory layer only needed "give me the best matching notes", using QMD directly would be the obvious answer.

## 18.2 Why QMD may be the wrong abstraction for memory ranking

The proposed memory architecture wants to build its **own final ranker**. The desired score is not merely text relevance.

A future ranking pipeline may need something like:

```text
raw lexical relevance
        |
        +-- recency decay
        +-- memory importance
        +-- active/superseded status
        +-- project/repository scope
        +-- exact filename/symbol/tag boost
        +-- provenance/trust constraints
        +-- previous recall utility
        |
        v
final memory score
        |
        v
optional diversity / MMR
```

If QMD internally performs its own re-ranking before returning results, two problems arise:

1. **The original BM25 signal may no longer be available in a clean form.**
2. **We would be stacking our ranker on top of another ranker whose effects may not be controllable or observable.**

That is undesirable for a memory system where ranking behavior is part of the design and where recall telemetry is later fed back into promotion decisions.

In particular, a result promoted from rank 30 to rank 3 by QMD's internal reranker is semantically different from a result that was rank 3 under raw BM25. If the Dreaming cycle later interprets retrieval rank or relevance as evidence of utility, that distinction matters.

Therefore the assumption "we already have QMD, so retrieval is solved" should be weakened to:

> QMD is useful for interactive vault search, but the memory subsystem may need a lower-level candidate-retrieval API that exposes raw lexical scores and allows AgentScheduler to own final ranking.

## 18.3 Candidate architecture

A clean approach would be:

```text
Markdown files
     |
     v
BM25/FTS index
     |
     | raw score + path + fields
     v
AgentScheduler ranker
     |
     +-- recency
     +-- importance
     +-- status
     +-- scope
     +-- recall history
     +-- trust/provenance
     v
final top-K
```

`ripgrep` can remain a separate exact-search path for symbols, filenames, commands, error strings, and debugging queries.

## 18.4 Do not implement BM25 from scratch unless necessary

BM25 itself is not difficult to describe, but building a production-quality text index also involves:

- tokenization
- document statistics
- inverted indexes
- incremental updates
- field weighting
- Unicode/language handling
- persistence
- deletion/update semantics
- query parsing

Reimplementing all of that purely to obtain BM25 scores would be wasted effort unless the vault is extremely small.

AgentScheduler is written in Go, so the most relevant current options are:

### Option A: Bleve

[Bleve](https://github.com/blevesearch/bleve) is a mature Go-native indexing/search library. Its current feature set includes BM25 scoring, text/numeric/date fields, query-time boosting, and hybrid/vector capabilities.

Advantages:

- native Go dependency
- persistent local index
- BM25 available directly
- field-aware queries and boosts
- metadata can be indexed alongside Markdown body
- no separate daemon required
- enough flexibility to keep final memory ranking in AgentScheduler

Potential architecture:

```text
Bleve BM25 -> top N candidates -> custom Go memory ranker -> top K
```

This is currently the most obvious implementation candidate to prototype first.

Source:

- https://github.com/blevesearch/bleve

### Option B: small dedicated Go BM25 library

A smaller library such as [`crawlab-team/bm25`](https://github.com/crawlab-team/bm25) implements several BM25 variants directly in Go.

Advantages:

- simpler scoring layer
- easy to understand and control
- useful for a proof of concept

Disadvantages:

- AgentScheduler would still need to own or build corpus indexing, incremental updates, tokenization policy, persistence, and metadata filtering
- less attractive as the vault grows

This is useful if the goal is a minimal prototype to test ranking formulas, but less compelling as a complete retrieval backend.

Source:

- https://github.com/crawlab-team/bm25

### Option C: Tantivy through Go bindings

Tantivy is a high-performance Rust search library with BM25. [`anyproto/tantivy-go`](https://github.com/anyproto/tantivy-go) exposes Go bindings and is used by Anytype.

Advantages:

- strong full-text search engine
- high-performance indexing
- BM25 is native to Tantivy

Disadvantages:

- Rust/CGo/native-library integration
- more complex build and distribution story
- probably excessive for a first AgentScheduler implementation

This is worth considering only if Bleve becomes a measurable bottleneck or search quality/performance requirements grow substantially.

Sources:

- https://github.com/anyproto/tantivy-go
- https://github.com/quickwit-oss/tantivy

## 18.5 Recommended experiment before choosing

Do not choose QMD or replace it based on architecture aesthetics alone. Build a small retrieval evaluation corpus from real vault queries.

For roughly 30-100 representative queries, record:

- expected useful notes
- raw BM25 rank
- QMD rank
- rank after custom metadata/recency/scope scoring
- latency
- index update cost

Test at least these query categories:

```text
exact filename/symbol
error message
project architecture question
user preference
recent project state
old evergreen lesson
superseded decision
cross-project generic knowledge
```

Then compare:

```text
QMD final results
vs
Bleve/raw BM25 + custom ranker
```

The evaluation question is not merely "which search engine has higher relevance?" It is:

> Which candidate-retrieval layer gives the memory system enough control and observability to produce reliable final ranking and trustworthy recall telemetry?

## 18.6 Current recommendation

Treat QMD as **provisional**, not foundational.

For the first serious memory prototype, evaluate **Bleve BM25 as candidate generation plus a custom AgentScheduler ranker**. Keep QMD for interactive/manual vault search unless the evaluation shows that it can expose sufficiently raw ranking data to support the same design.

This avoids the awkward architecture of custom re-ranking on top of opaque pre-reranking and keeps the promotion feedback loop interpretable.

---

# 19. Proposed final retrieval score

Assuming the system has access to a raw lexical relevance score, start with a transparent formula.

For example:

```text
base = normalize(bm25)

final = base
      * recency_factor
      * importance_factor
      * status_factor
      * trust_factor
      + project_scope_boost
      + exact_identifier_boost
```

Alternatively use a weighted additive score if multiplicative suppression proves too aggressive.

The exact formula matters less than these properties:

- every component is observable
- raw lexical relevance remains available
- the system can explain why a result ranked highly
- ranking can be replayed offline
- telemetry does not depend on hidden upstream ranking

Coding-agent search benefits unusually strongly from exact lexical matches because filenames, symbols, CLI commands, error strings, and project identifiers matter.

## 19.1 Recency

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

# 20. Recall telemetry as promotion evidence

The most valuable idea to copy from OpenClaw/Codex is to log actual retrieval usage.

Each recall can produce a tiny event such as:

```json
{
  "path": "Daily/2026-08-12.md",
  "query": "how is auth deployed",
  "raw_bm25": 7.4,
  "final_score": 0.86,
  "rank": 2,
  "timestamp": "2026-08-21T15:42:00Z",
  "project": "AgentScheduler"
}
```

Keeping both `raw_bm25` and `final_score` is important. It makes ranking feedback interpretable.

The Dreaming job can then derive:

- recall count
- unique-query count
- cross-day recurrence
- average lexical relevance
- average final rank
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

# 21. Promotion score

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

# 22. Safe consolidation rules

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

# 23. What not to do

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

## Stack opaque rerankers

If QMD or another search layer has already significantly reranked the corpus, applying a second custom memory ranker can make relevance and recall telemetry difficult to interpret. Prefer raw candidate scores where possible.

## Reimplement a search engine unnecessarily

If direct control over BM25 is required, use a maintained library such as Bleve before writing tokenization, indexing, persistence, and BM25 machinery from scratch.

## Regenerate the whole wiki nightly

Prefer incremental consolidation. Large generative rewrites create accidental information loss and noisy Git history.

---

# 24. Proposed AgentScheduler target architecture

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
+-- BM25 index            raw candidate generation
|    `-- likely prototype: Bleve
+-- custom ranker         recency/scope/status/importance/telemetry
`-- optional diversity   MMR or similar

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

QMD can remain alongside this for interactive/manual vault search unless evaluation shows that it exposes a sufficiently controllable raw ranking layer.

The main architectural stance is:

> **Do not replace Markdown with a memory platform. Add a memory lifecycle and controllable retrieval layer over Markdown.**

---

# 25. Concrete requirements for a follow-up implementation agent

A follow-up agent should work from these requirements:

1. Markdown remains the source of truth.
2. Existing Daily/project/wiki files remain canonical evidence and durable knowledge.
3. `ripgrep` remains available for exact code/project strings.
4. Do not assume QMD is the final memory retrieval engine; evaluate whether its internal reranking conflicts with the need for a custom ranker.
5. Prefer a retrieval primitive that exposes raw lexical/BM25 scores and metadata.
6. Prototype Bleve before implementing BM25/indexing infrastructure from scratch.
7. Session extraction and global consolidation must be separate processes.
8. A session may legitimately produce zero memory candidates.
9. The extraction gate is: **Would this plausibly change how a future agent acts?**
10. Candidate categories should include decisions, constraints, preferences, validated facts, lessons, failure patterns, procedures, open questions, and project state.
11. Dreaming should separate extraction/staging, reflection, and durable consolidation.
12. Consolidation must explicitly support `ADD`, `UPDATE`, `MERGE`, `SUPERSEDE`, `ARCHIVE`, and `IGNORE`.
13. No automatic hard deletion of memory.
14. Every durable claim should retain provenance to source evidence.
15. Conflicts should be resolved using evidence strength, explicit corrections, recurrence, and recency; never recency alone.
16. If conflict remains unresolved, mark it disputed/needs-review rather than fabricating certainty.
17. Retrieval usage should be logged and used as promotion evidence.
18. Store raw retrieval score and final memory score separately in telemetry.
19. Repeated recall and query diversity are strong signals that episodic knowledge deserves promotion.
20. Explicit durable decisions/constraints may bypass recall-count gates.
21. Episodic memory may decay in retrieval score.
22. Durable curated knowledge should not decay merely because it is old.
23. Directory/memory type should control decay and consolidation policy.
24. Always-loaded/core memory should have a strict size budget.
25. Project scope, directory, repository, filename, symbol, and tags should be available as ranking boosts.
26. Consolidation should make minimal, incremental edits rather than regenerate whole notes.
27. Existing Git history should be used as rollback/audit support.
28. A human-readable Dreaming report should explain promotions, merges, supersessions, and unresolved conflicts.
29. External/untrusted content must not automatically become durable instructions.
30. Before choosing the search stack, benchmark QMD against raw BM25 + custom ranking on representative real vault queries.

---

# 26. Recommended implementation experiments

Before building the full lifecycle, run three small experiments.

## Experiment 1: retrieval control

Build a tiny Bleve index over a representative subset of Markdown files.

Return:

```text
path
raw BM25 score
summary
last_updated
folder/project
tags
status
```

Implement a simple custom reranker using recency and project scope.

Compare results against QMD for representative queries.

## Experiment 2: recall telemetry

Log all memory queries and which results are actually injected/read.

After one or two weeks, inspect whether:

- frequently recalled notes look genuinely promotion-worthy
- query diversity is informative
- raw BM25 score correlates with usefulness
- custom reranking improves recent project-state retrieval without hiding evergreen knowledge

## Experiment 3: Dreaming without writes

Run Extract + Reflect in report-only mode.

Generate candidate actions such as:

```text
ADD
MERGE
SUPERSEDE
IGNORE
```

but do not modify durable knowledge yet.

Manually inspect precision before allowing autonomous writes.

This separates three risks: retrieval quality, telemetry quality, and consolidation quality.

---

# 27. Recommended source systems to copy from

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
- safe consolidation validation

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

Source:

- https://github.com/vectorize-io/hindsight

## Basic Memory

Best source for:

- Markdown observations and relations
- wiki-style entity graph without requiring a graph database as source of truth

Sources:

- https://github.com/basicmachines-co/basic-memory
- https://github.com/basicmachines-co/basic-memory/blob/main/docs/semantic-search.md

## Retrieval implementation candidates

### Bleve

Go-native indexing/search library with BM25, field mapping, query boosting, persistence, and optional vector/hybrid functionality.

- https://github.com/blevesearch/bleve

### crawlab-team/bm25

Small Go implementation of several BM25 variants. Useful for experiments but does not replace a complete persistent search index.

- https://github.com/crawlab-team/bm25

### Tantivy / Tantivy-Go

High-performance Rust full-text engine with BM25 plus Go bindings, at higher integration/build complexity.

- https://github.com/quickwit-oss/tantivy
- https://github.com/anyproto/tantivy-go

## Research

- Generative Agents — relevance + recency + importance + reflection: https://arxiv.org/abs/2304.03442
- MemoryBank — forgetting/refresh concepts: https://arxiv.org/abs/2305.10250
- A-MEM — evolving Zettelkasten-like agent memory: https://arxiv.org/abs/2502.12110
- Sleep-time Compute — move preparation/reflection off the interactive critical path: https://arxiv.org/abs/2504.13171

---

# 28. Bottom line

AgentScheduler already has the right primitive ingredients: scheduled jobs, raw session exports, durable Markdown, and a small always-loaded memory file.

The next meaningful step is not another storage backend. It is to turn the current compression workflow into a **provenance-aware, usage-aware Dreaming cycle**:

```text
session evidence
    -> extract only high-signal candidates
    -> retrieve through a controllable lexical layer
    -> record what is actually recalled
    -> reflect across days/sessions
    -> qualify promotion deterministically
    -> consolidate minimally
    -> supersede/archive rather than erase
    -> retain provenance and audit trail
```

The important retrieval caveat is now explicit: **QMD should not automatically be treated as the memory engine merely because it is already installed.** If its internal re-ranking prevents access to clean candidate scores or makes our own ranking/telemetry opaque, a lower-level BM25 implementation is architecturally cleaner. In a Go codebase, Bleve is the first candidate worth prototyping; a small BM25 library is suitable for experiments, and Tantivy-Go is a higher-complexity option if performance eventually demands it.

This combines the strongest parts of Codex, OpenClaw, llm-wiki-memory, and Hindsight while preserving the transparency and editability of plain Markdown and keeping the final memory ranking under AgentScheduler's control.
