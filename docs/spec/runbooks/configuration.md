# Configuration

Links:
- [Spec index](../spec-index.md)
- [Getting Started](getting-started.md)

## Environment Variables

| Variable | Required | Default | Description |
| --- | --- | --- | --- |
| `TELEGRAM_BOT_TOKEN` | Yes | - | Telegram bot token |
| `OPENCODE_BASE_URL` | No | `http://localhost:4096` | Base URL for Opencode |
| `OPENCODE_AUTH_TOKEN` | No | - | Optional Bearer token for Opencode |
| `ALLOWED_TELEGRAM_IDS` | No | empty | Comma/space separated allowed users |
| `ADMIN_TELEGRAM_IDS` | No | empty | Comma/space separated admin users |
| `SESSION_PREFIX` | No | `oct_` | Prefix used for persistent session |
| `TELEGRAM_MODE` | No | `polling` | Polling supported; webhook not implemented |
| `PORT` | No | `3000` | Reserved port for webhook mode |
| `REDIS_URL` | No | - | Reserved for future persistent store |

## Parsing Rules

- ID lists support both formats: `1,2,3` and `1 2 3`.
- Empty `ALLOWED_TELEGRAM_IDS` means allow all users.
- App exits if `TELEGRAM_BOT_TOKEN` is missing.

## Example

```env
TELEGRAM_BOT_TOKEN=123456:ABCDEF
OPENCODE_BASE_URL=http://localhost:4096
OPENCODE_AUTH_TOKEN=
ALLOWED_TELEGRAM_IDS=123456789
ADMIN_TELEGRAM_IDS=123456789
SESSION_PREFIX=oct_
TELEGRAM_MODE=polling
PORT=3000
REDIS_URL=
```
