# Review Notes: Agent Memory Dreaming Research

_Date: 2026-08-21_

These notes record design decisions and open questions after reviewing [`agent-memory-dreaming-research.md`](agent-memory-dreaming-research.md). They intentionally do not rewrite the underlying research report. Where these notes choose a concrete direction, they should be treated as the current implementation preference for follow-up work.

## Decisions

### Use recall frequency as a ranking and promotion signal

Adopt the OpenClaw idea that actual memory usage matters. The number of times a note or memory is recalled should contribute to its weight, rather than relying only on an LLM-assigned importance score.

Relevant signals should include at least:

- recall count
- query diversity where useful
- recency of recall

Repeated successful retrieval is evidence that a memory is useful to future sessions and can therefore also be a signal for promotion into more durable knowledge.

### Use a simple 30-day half-life decay

Use the OpenClaw-style exponential recency decay with a **30-day half-life** for episodic/retrieval memory:

```text
recency(age) = 0.5 ^ (age_days / 30)
```

Do not introduce a more complicated decay model unless real usage shows that the simple 30-day model is insufficient.

This decay affects ranking, not the existence of the Markdown file. Durable/curated knowledge does not need to disappear merely because it is old.

### Do not reproduce Hindsight's full memory model

Do not implement a complex Hindsight-style observation/mental-model infrastructure or database-backed memory model.

The useful part to retain from Hindsight and related projects is the **conceptual idea of synthesizing higher-level knowledge from evidence spread across multiple individual sessions**.

The desired system should stay substantially simpler and remain compatible with the existing Markdown/Obsidian architecture.

## Desired cross-session synthesis

We want to experiment with turning several related session-level observations into a larger, more durable concept.

For example:

```text
Session A: preference / observation
Session B: similar correction
Session C: same pattern in another context
        |
        v
Higher-level concept or durable note
```

The resulting concept should be grounded in the underlying sessions rather than being an unsupported abstraction invented by the consolidating model.

This is an area where further research is required before implementation.

## Open research question: consolidation prompts

Research good public prompts and prompt patterns for cross-session synthesis, especially prompts that reliably decide:

- which observations belong to the same concept
- when repeated examples justify a generalization
- how specific the resulting concept should remain
- how supporting evidence should be retained
- when two observations are complementary versus contradictory
- when the model should decline to generalize
- how existing knowledge should be incorporated without causing accidental information loss

Relevant systems to inspect further include OpenClaw, OpenAI Codex, llm-wiki-memory, Hindsight, A-MEM, and other systems with publicly available consolidation/reflection prompts.

Do not invent the final consolidation prompt from first principles before this follow-up research has been done.

## Open design question: update existing notes or create superseding notes?

The storage mutation strategy is not decided yet.

Two main approaches should be evaluated:

### Option A: update an existing note

When new evidence refines an existing concept, edit the existing Markdown note in place while preserving provenance/history through Git and source references.

Potential advantages:

- one obvious current source of truth
- simpler retrieval
- fewer near-duplicate notes

Potential risks:

- consolidation mistakes can damage a good existing note
- historical evolution becomes less explicit inside the vault
- broad LLM rewrites may accidentally remove useful detail

### Option B: create a new note and supersede the old one

Write a new consolidated note and mark the previous note as superseded, for example through frontmatter or links.

Potential advantages:

- old knowledge remains intact and auditable
- safer autonomous consolidation
- explicit historical evolution

Potential risks:

- more files and metadata
- retrieval must reliably filter or down-rank superseded notes
- concepts may fragment over time if supersession is overused

This decision should be made after investigating how the strongest existing systems handle consolidation in practice and after testing both approaches on real Obsidian notes.

## Current preferred direction for the next iteration

Keep the system deliberately simple:

```text
BM25 / lexical relevance
        +
30-day recency decay
        +
recall-frequency signal
        |
        v
retrieval / promotion ranking

multiple related sessions
        |
        v
LLM consolidation / concept synthesis
        |
        v
Markdown durable knowledge
```

The ranking side should remain deterministic and understandable. The LLM should primarily be used where semantic judgment is actually needed: extracting useful information, finding patterns across sessions, forming higher-level concepts, and reconciling evidence.

The next research step is therefore **not** to design a more complex memory architecture. It is to find and compare strong real-world prompts for the cross-session consolidation step and then decide whether consolidation mutates existing notes or produces new superseding notes.