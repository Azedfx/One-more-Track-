# syntax=docker/dockerfile:1

# ---- build stage ----------------------------------------------------------
FROM golang:1.24-alpine AS build
WORKDIR /src

# No third-party modules (stdlib only), so go.mod is enough for caching.
COPY go.mod ./
COPY cmd ./cmd
COPY internal ./internal

# Static, stripped binary. Templates and static assets are go:embed'd into it.
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/server ./cmd/server

# ---- runtime stage --------------------------------------------------------
FROM alpine:3.20
# ca-certificates: outbound HTTPS to Bitget. tzdata: correct UTC date handling.
RUN apk add --no-cache ca-certificates tzdata wget \
    && adduser -D -u 10001 app
WORKDIR /app
COPY --from=build /out/server /app/server

# Price cache lives here; a volume keeps it across restarts.
RUN mkdir -p /app/.cache && chown -R app:app /app
USER app

ENV PORT=8080
EXPOSE 8080
VOLUME ["/app/.cache"]

# /health returns 200 without triggering a price download.
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD wget -qO- "http://127.0.0.1:${PORT}/health" >/dev/null 2>&1 || exit 1

ENTRYPOINT ["/app/server"]
