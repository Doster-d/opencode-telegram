# Telegram Backend/Daemon MVP

Links:
- [Spec index](../spec-index.md)
- [Telegram Command Specification](telegram-commands.md)

Spec Namespace: SPEC-TGDAEMON
Status: Draft
Version: 0.2
Owners: Maintainers
Last Updated: 2026-02-10

## Scope

Minimal control-plane MVP for Telegram -> backend -> local daemon. Covers strict command contracts, pairing, long-poll delivery via Redis, result posting, daemon lifecycle, and Telegram approval routing.

Out of scope: TUI open, inbound network access to agent, multi-backend clustering.

## Actors and Components

- Telegram Bot: shared bot for all users. Owns UX, approvals, and routes commands to backend.
- Backend: single instance. Stores bindings, policies, Redis queues, and delivers commands/results.
- Agent Daemon: local OS service. Polls backend, enforces permissions, runs OpenCode.

## Identity and Pairing

Identifiers:

- `telegram_user_id`: Telegram user id.
- `agent_id`: backend-issued UUID.
- `agent_key`: backend-issued bearer token for agent.

Pairing flow:

1. User runs `/pair` in Telegram. Bot calls `POST /v1/pair/start` to obtain `{ pairing_code, expires_at }`.
2. User enters `pairing_code` locally on the agent machine. Agent calls `POST /v1/pair/claim` with `{ pairing_code, device_info }`.
3. Backend returns `{ agent_id, agent_key }`. Agent persists `agent_key` locally.

Constraints:

- Pairing code TTL: 10 minutes. Expired or reused codes are rejected.
- Only one active agent per Telegram user in MVP. New pairing invalidates the previous agent.

## Projects and Permissions (Telegram-only)

Default-deny policy enforced locally by the daemon.

Registration:

- User runs `/project add <ABS_PATH>`.
- Backend enqueues `register_project` with `project_path_raw`.
- Agent validates and normalizes the path, computes `project_id`, and returns the result.

Policy model:

- `decision`: `ALLOW` or `DENY`.
- `expires_at`: RFC3339 or `null`.
- `scope`: fixed set of operations: `START_SERVER`, `RUN_TASK`.

Telegram approval options:

- Deny
- Allow 30m: `START_SERVER`
- Allow 30m: `START_SERVER + RUN_TASK`
- Allow until revoked: `START_SERVER + RUN_TASK`

Backend stores policies and delivers them to the agent via `apply_project_policy`.

## Project Identity

Path normalization:

- `project_path_raw` must be absolute.
- Agent computes `project_path = realpath(project_path_raw)`.
- Path encoding is UTF-8.

Project id:

- `project_id = hex(sha256(bytes(agent_id) || 0x0A || bytes(project_path)))`.

Forbidden paths rejected by agent during `register_project`:

- Root directories (`/`, `C:\`).
- User home directory.
- System directories:
  - Linux: `/etc`, `/bin`, `/usr`, `/var`.
  - macOS: `/System`, `/Library`.
  - Windows: `C:\Windows`, `C:\Program Files`.

## Command Contract

Agent accepts only these command types:

- `register_project`
- `apply_project_policy`
- `start_server`
- `run_task`
- `status`

Shared command format (strict JSON decoding, reject unknown fields/types):

```json
{
  "command_id": "uuid",
  "idempotency_key": "string",
  "type": "register_project|apply_project_policy|start_server|run_task|status",
  "created_at": "RFC3339",
  "payload": {}
}
```

Command execution rules:

- Mutating commands are serialized (one at a time).
- `status` is read-only and returns immediately.
- Unknown `type` yields `ERR_COMMAND_UNKNOWN`.
- Strict payload schema per command type; invalid payload yields `ERR_COMMAND_INVALID`.

Idempotency:

- Agent keeps a replay cache of the last 1000 `idempotency_key` values for 24 hours.
- Duplicate `idempotency_key` returns cached result without re-execution.

## OpenCode Lifecycle (Daemon)

`start_server`:

- Server runs in `cwd = project_path`.
- Command: `opencode serve --hostname 127.0.0.1 --port <port>`.
- Readiness check: `GET http://127.0.0.1:<port>/global/health` must return HTTP 200.
- Readiness timeout: 10 seconds; on timeout, terminate process and return `ERR_START_TIMEOUT`.

Port allocation:

- Fixed range: `4096..4196`.
- One server per project. If already running, return success with current port.
- If no ports available, return `ERR_PORT_EXHAUSTED`.

`run_task`:

- Ensures server is running (calls `start_server` as a sub-operation).
- Command: `opencode run --attach http://127.0.0.1:<port> <prompt>`.

Execution timeout: 600 seconds per command.

## Backend API

Authentication:

- Agents authenticate with `Authorization: Bearer <agent_key>`.

Endpoints:

- `POST /v1/pair/start` (bot) -> `{ pairing_code, expires_at }`.
- `POST /v1/pair/claim` (agent) -> `{ agent_id, agent_key }`.
- `GET /v1/poll?timeout_seconds=25` (agent) -> `200 { command: <Command> }` or `204`.
- `POST /v1/result` (agent) -> `{ ok: true }`.

Result payload:

```json
{
  "command_id": "uuid",
  "ok": true,
  "error_code": "ERR_DOMAIN_REASON",
  "summary": "string",
  "stdout": "string",
  "stderr": "string",
  "meta": {}
}
```

Limits:

- `stdout` max 64 KiB.
- `stderr` max 64 KiB.
- `summary` max 2 KiB.
- Result TTL in Redis: 14 days.

## Redis Queue Semantics

Keys:

- Command queue: LIST `oct:cmd:<agent_id>`.
- Inflight queue: LIST `oct:inflight:<agent_id>`.
- Result storage: STRING `oct:result:<agent_id>:<command_id>`.

Delivery (at-least-once):

- Backend enqueues commands via `LPUSH` to `oct:cmd:<agent_id>`.
- On poll, backend runs `BRPOPLPUSH oct:cmd:<agent_id> oct:inflight:<agent_id> timeout_seconds`.
- If a command is returned, it is delivered to the agent; otherwise respond `204`.

Result handling:

- On `POST /v1/result`, backend removes the exact command string from inflight and stores the result.

Redelivery:

- Backend tracks `inflight_at` per inflight entry.
- If inflight age exceeds `REDELIVERY_AFTER_SECONDS = 120`, the command is eligible for redelivery on the next poll.

## Telegram Bot Routing and Approvals

Commands (MVP):

- `/pair`
- `/project add <ABS_PATH>`
- `/project list`
- `/start_server <project>`
- `/run <project> <prompt>`
- `/status`

Routing:

- Bot validates inputs and routes to backend. Backend enqueues agent commands.
- Project selector `<project>` is an alias unique per user. Alias is user-provided or derived from directory name.

Approval flow:

- When a project is first registered, its policy defaults to `DENY`.
- If an operation is attempted without required scope or after expiration, bot must prompt with the fixed approval options.
- Backend persists the decision and emits `apply_project_policy` to the agent.

Result delivery:

- Backend forwards result summaries and errors to the Telegram user.
- Bot formats `summary` + truncated stdout/stderr (respecting limits) for display.

## Error Taxonomy

Errors use `ERR_<DOMAIN>_<REASON>` format. Minimum set in MVP:

- `ERR_COMMAND_UNKNOWN`
- `ERR_COMMAND_INVALID`
- `ERR_AUTH_UNAUTHORIZED`
- `ERR_PAIRING_EXPIRED`
- `ERR_PAIRING_REUSED`
- `ERR_POLICY_DENIED`
- `ERR_PATH_FORBIDDEN`
- `ERR_PATH_INVALID`
- `ERR_PORT_EXHAUSTED`
- `ERR_START_TIMEOUT`

## Acceptance Criteria (BDD-ready)

- `AC-MVP-01` (`SPEC-TGDAEMON-001`): Shared command/result schemas use strict JSON decoding, reject unknown fields and command types, and return errors in `ERR_<DOMAIN>_<REASON>` format.
- `AC-MVP-02` (`SPEC-TGDAEMON-002`): Backend exposes `POST /v1/pair/start`, `POST /v1/pair/claim`, `GET /v1/poll`, and `POST /v1/result` with bearer auth for poll/result and proper 200/204 behavior.
- `AC-MVP-03` (`SPEC-TGDAEMON-003`): Redis queue uses `LPUSH` + `BRPOPLPUSH` with an inflight list, removes inflight on result, and supports redelivery after 120s.
- `AC-MVP-04` (`SPEC-TGDAEMON-004`): Daemon enforces allowed command dispatcher, strict payload validation, and idempotency replay cache (1000 keys, TTL 24h).
- `AC-MVP-05` (`SPEC-TGDAEMON-005`): Daemon serializes mutating commands, allows immediate `status`, and allocates ports in `4096..4196` with `ERR_PORT_EXHAUSTED` on exhaustion.
- `AC-MVP-06` (`SPEC-TGDAEMON-006`): Pairing codes expire at 10 minutes and only one active agent remains per Telegram user after re-pairing.
- `AC-MVP-07` (`SPEC-TGDAEMON-007`): Telegram approvals are required for project access and operations, with fixed decision set and backend-delivered policy updates.
- `AC-MVP-08` (`SPEC-TGDAEMON-008`): `start_server` readiness uses `/global/health` with a 10s timeout and returns `ERR_START_TIMEOUT` on failure; `run_task` ensures server is running before execution.
