# Error Knowledge Base

## KB-2026-02-09-01 Terminal event coverage risk [OPEN] [MUST FIX]

### Symptoms
- Active run can remain stuck when terminal events arrive with statuses/types outside the current matcher.

### Root Cause
- `internal/bot/events.go` only treats `session.updated` with `completed|failed` as terminal, so other terminal forms are ignored.

### Fix
- Expand terminal detection to handle the full set of terminal event shapes/statuses used by the upstream runtime (for example terminal `session.updated` variants and any terminal `session.completed`-style event type if emitted).
- Ensure terminal handling is centralized in one helper so future status additions are easy to include.

### Prevention
- Maintain an explicit allowlist/constants for terminal states and update it whenever upstream event contracts change.
- Add contract-focused tests that fail when unknown terminal forms are not recognized.

### Reproduce/Verify
- Reproduce: feed an event stream where completion is signaled with a terminal status/type not currently matched and observe the run never transitions to terminal in bot state.
- Verify: after fix, the same stream marks the run terminal and clears any active-run tracking.

### Regression Test Reference
- Add/maintain: `internal/bot/events_test.go` terminal matcher coverage test for alternate terminal statuses/types (including at least one previously missed terminal form).

## KB-2026-02-09-02 Run owner key collision risk [OPEN] [MUST FIX]

### Symptoms
- Ownership can be overwritten when one `session_id` is reused concurrently across multiple `(chat_id,user_id)` contexts.

### Root Cause
- `runOwners` in `internal/bot/telegram.go` stores one entry per `session_id -> runKey`, so concurrent reuse causes collisions/last-write-wins behavior.

### Fix
- Replace one-to-one `session_id` ownership mapping with a collision-safe key strategy that preserves context (for example composite ownership key including `chat_id` and `user_id`, or a multimap keyed by `session_id` with scoped owner records).
- Update lookup/remove paths to use the same scoped identity consistently.

### Prevention
- Enforce owner identity invariants in code: no global overwrite when context differs.
- Add concurrency/scoped-ownership tests covering reused `session_id` across distinct chat/user pairs.

### Reproduce/Verify
- Reproduce: start two concurrent runs in different `(chat_id,user_id)` contexts that share `session_id`; observe owner mapping for one context gets replaced.
- Verify: after fix, both contexts retain correct ownership and callbacks/events resolve to the correct run owner.

### Regression Test Reference
- Add/maintain: `internal/bot/telegram_test.go` test for concurrent reused `session_id` across different `(chat_id,user_id)` scopes validating independent ownership resolution.
