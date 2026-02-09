# Telegram Command Specification

Links:
- [Spec index](../spec-index.md)
- [System Architecture](../system/architecture.md)

Spec Namespace: SPEC-CMD
Status: Accepted
Version: 1.0
Owners: Maintainers
Last Updated: 2026-02-09

## Overview

Defines supported Telegram commands, access rules, and expected user-visible responses.

## Commands

| Command | Access | Behavior |
| --- | --- | --- |
| `/status` | allowed users | replies with configured Opencode base URL |
| `/sessions` | allowed users | lists filtered sessions by `SESSION_PREFIX` |
| `/run <prompt>` | allowed users | sends prompt to persistent session |
| `/abort <session_id>` | admin only | aborts session |
| `/createsession [title]` | allowed users | creates and auto-selects new session |
| `/deletesession <id>` | admin only | deletes session |
| `/selectsession <id\|prefix>` | allowed users | selects session by id or title prefix |
| `/mysession` | allowed users | shows current selected session |

## Default Behaviors

- Non-command text is treated as `/run <text>`.
- Unknown command returns `Unknown command`.
- Disallowed users are ignored.

## Acceptance Criteria (BDD-ready)

- AC-1 (`SPEC-CMD-001`): Allowed user running `/status` gets a status message.
- AC-2 (`SPEC-CMD-002`): Non-admin user cannot execute admin commands.
- AC-3 (`SPEC-CMD-003`): Non-command text invokes run behavior.
- AC-4 (`SPEC-CMD-004`): Unknown command returns explicit fallback message.
