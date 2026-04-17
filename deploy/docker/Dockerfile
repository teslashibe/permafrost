# syntax=docker/dockerfile:1.7

# ──────────────────────────────────────────────────────────────────────────────
# Build stage
# ──────────────────────────────────────────────────────────────────────────────
FROM golang:1.25-alpine AS build

ARG VERSION=dev
ENV CGO_ENABLED=0 GOOS=linux GOFLAGS=-trimpath

WORKDIR /src

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY . .

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go build -ldflags "-s -w -X github.com/teslashibe/permafrost/internal/cli.Version=${VERSION}" \
        -o /out/permafrost  ./cmd/permafrost && \
    go build -ldflags "-s -w -X github.com/teslashibe/permafrost/internal/cli.Version=${VERSION}" \
        -o /out/permafrostd ./cmd/permafrostd

# ──────────────────────────────────────────────────────────────────────────────
# Runtime stage
# ──────────────────────────────────────────────────────────────────────────────
FROM alpine:3.20

RUN apk add --no-cache ca-certificates tzdata && \
    addgroup -S permafrost && adduser -S -G permafrost permafrost

WORKDIR /app
COPY --from=build /out/permafrost  /usr/local/bin/permafrost
COPY --from=build /out/permafrostd /usr/local/bin/permafrostd

USER permafrost

EXPOSE 8080

ENTRYPOINT ["permafrostd"]
