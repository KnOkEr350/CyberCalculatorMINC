FROM golang:1.27.1-bookworm@sha256:648f440f42a0958804efb24df176f806f9d353b41f1c0627f666428e40310f6b AS builder
ARG APP_VERSION=dev
WORKDIR /src

COPY backend/go.mod backend/go.sum* ./
RUN go mod download

COPY backend/ .
COPY frontend/index.html frontend/app.js frontend/workspace.js frontend/mfa.js frontend/features.js frontend/screens.js frontend/okz.js frontend/style.css frontend/theme.css /tmp/frontend/
COPY frontend/core /tmp/frontend/core
COPY frontend/shell /tmp/frontend/shell
COPY frontend/screens /tmp/frontend/screens
COPY frontend/components/ui.js /tmp/frontend/components/ui.js
RUN rm -rf internal/modules/webapp/dist \
    && mkdir -p internal/modules/webapp/dist/components \
    && cp /tmp/frontend/index.html /tmp/frontend/app.js /tmp/frontend/workspace.js /tmp/frontend/mfa.js /tmp/frontend/features.js /tmp/frontend/screens.js /tmp/frontend/okz.js /tmp/frontend/style.css /tmp/frontend/theme.css internal/modules/webapp/dist/ \
    && cp -R /tmp/frontend/core /tmp/frontend/shell /tmp/frontend/screens internal/modules/webapp/dist/ \
    && cp /tmp/frontend/components/ui.js internal/modules/webapp/dist/components/ \
    && printf '%s\n' "$APP_VERSION" > internal/modules/webapp/dist/version.txt
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/server ./cmd/server

# ---- Stage 2: минимальный рантайм-образ ---------------------------------
FROM gcr.io/distroless/static-debian12:nonroot@sha256:afa5c872c891853ca7fcf1f12c3edb23f7eeef36189728842dd51042ff57f7ab AS runtime
WORKDIR /app

COPY --from=builder /out/server /app/server
COPY migrations /app/migrations

USER nonroot:nonroot
EXPOSE 8080
# The HTTP healthcheck is configured on the backend Compose service; this
# image is also used for the non-HTTP worker and one-shot migration command.
ENTRYPOINT ["/app/server"]
