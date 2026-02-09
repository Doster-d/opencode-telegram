---
description: "Request a feature/change: Ticket → Spec → Acceptance Criteria → Plan (REVIEW by default)"
agent: orchestrator
model: openai/gpt-5.3-codex
temperature: 0.2
---

# Request

**Goal**: $ARGUMENTS

## Protocol

### Phase 0) Default mode

Unless the user explicitly switched to WORK, treat `/request` as **REVIEW**:

- No repo mutations.
- Output is a **Ticket + Spec + Acceptance Criteria + Plan**.

### Phase 1) Create a Task Ticket (Orchestrator)

Create a short ticket in the response (keep it copy/paste friendly):

```md
# Ticket: <short-name>

## Goal
<what we want>

## Non-Goals
- <what we are NOT doing>

## Acceptance Criteria
- [ ] <externally observable behavior>

## Constraints
- runtime:
- versions:
- performance:
- security:

## Artifacts
- spec: docs/spec/<name>.md
- tests: <path>
- code: <path>

## Plan / TODO
1) …

## Risks / Unknowns
- …
```

### Phase 2) Skill discovery (lazy-load)

Do **not** load everything. Discover broadly, load narrowly.

1) Run a targeted skill search:
   - Search terms: domain keywords + the goal + language/framework names.
2) Load only the skills required for this request:
   - Always for non-trivial work: `spec-specification`, `spec-bdd`
   - If debugging/regression risk is high: `spec-audit`
   - If tests are needed: `spec-tdd` (unit boundaries), `spec-verify` (commands)
   - If env/run commands unclear: `spec-env-scout`
   - If dependencies/versions matter: `spec-deps`
   - If DB schema changes: `spec-migrations`
   - If security surface expands: `spec-threat-model`

### Phase 3) Specification (Architect)

Produce a spec that is:

- testable
- implementation-agnostic
- explicit about edge cases and failures

Minimum spec structure:

```md
# <Feature / Change>

## Overview

## User-visible behavior

## Inputs / Outputs

## Edge cases

## Errors & failure modes

## Compatibility / migration notes

## Acceptance Criteria (BDD-ready)
```

### Phase 4) QA plan (QA)

Output a short verification plan:

- what to test (layers)
- what might break (regression zones)
- minimal commands to verify

### Phase 5) If WORK is enabled

If the user switched to WORK mode, then proceed:

1) Programmer: implement smallest correct change + unit tests
2) QA: run tests/lint/build and report evidence
3) Security: if any meaningful failure occurred, record it in error-kb

## Non-Negotiable Deliverables

- [ ] Task Ticket with explicit scope + acceptance criteria
- [ ] Spec (or spec update) aligned with the ticket
- [ ] BDD-ready acceptance criteria
- [ ] Verification plan (commands + regression zones)
- [ ] In WORK: tests + implementation + evidence
- [ ] If a meaningful mistake occurred → update `docs/agent/error-kb.md`

## Mindset

- Build constraints first, then build features
- Prefer explicit acceptance criteria over vibes
- If you can't test it, you don't understand it
- Stop after REVIEW deliverables unless WORK is explicitly enabled

