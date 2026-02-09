# System Architecture

Links:
- [Spec index](../spec-index.md)
- [Telegram Command Specification](../features/telegram-commands.md)

## Components

- `cmd/opencode-bot/main.go`: bootstrap config, clients, polling/event loops
- `internal/bot/telegram.go`: command routing and handlers
- `internal/bot/opencode_client.go`: HTTP + SSE interaction with Opencode
- `internal/bot/events.go`: event handling and Telegram message edits
- `pkg/store`: in-memory mapping for sessions/messages/users

## Runtime Flow

```mermaid
sequenceDiagram
  autonumber
  participant TG as "Telegram"
  participant APP as "BotApp"
  participant OC as "Opencode"
  participant ST as "Store"

  TG->>APP: "/run <prompt>"
  APP->>TG: "Running on Opencode..."
  APP->>ST: "save session -> message mapping"
  APP->>OC: "POST /session/{id}/message"
  OC-->>APP: "SSE message.part.updated"
  APP->>OC: "GET /session/{id}/message"
  APP->>TG: "edit message text"
```

## State Model

```mermaid
stateDiagram-v2
  direction LR
  [*] --> ST_INIT
  state "Initialized" as ST_INIT
  state "Polling" as ST_POLL
  state "Handling Command" as ST_CMD
  state "Streaming Updates" as ST_STREAM
  state "Error" as ST_ERR

  ST_INIT --> ST_POLL: "start polling"
  ST_POLL --> ST_CMD: "incoming message"
  ST_CMD --> ST_STREAM: "run/prompt success"
  ST_STREAM --> ST_POLL: "stream completed"
  ST_CMD --> ST_ERR: "command/API failure"
  ST_ERR --> ST_POLL: "recover and continue"
```
