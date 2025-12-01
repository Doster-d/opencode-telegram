FROM golang:1.20-alpine AS builder
RUN apk add --no-cache git
WORKDIR /src
COPY . .
ENV CGO_ENABLED=0
RUN go build -o /out/opencode-bot -ldflags "-s -w" ./cmd/opencode-bot

FROM scratch
COPY --from=builder /out/opencode-bot /opencode-bot
EXPOSE 3000
ENTRYPOINT ["/opencode-bot"]
