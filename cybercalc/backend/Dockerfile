# syntax=docker/dockerfile:1
#
# Собирается с контекстом = корень репозитория (см. docker-compose.yml:
# build.context: . , build.dockerfile: backend/Dockerfile), чтобы в финальный
# образ можно было положить и код, и миграции, и статику одним Dockerfile.

# ---- Stage 1: сборка ---------------------------------------------------
FROM golang:1.22-bookworm AS builder
WORKDIR /src

COPY backend/go.mod backend/go.sum* ./
RUN go mod download

COPY backend/ .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/server ./cmd/server

# ---- Stage 2: минимальный рантайм-образ ---------------------------------
FROM gcr.io/distroless/static-debian12:nonroot AS runtime
WORKDIR /app

COPY --from=builder /out/server /app/server
COPY migrations /app/migrations
COPY backend/web /app/web

USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/app/server"]
