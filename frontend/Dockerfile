FROM alpine:3.24.1@sha256:28bd5fe8b56d1bd048e5babf5b10710ebe0bae67db86916198a6eec434943f8b AS builder

ARG APP_VERSION=dev
WORKDIR /src
COPY index.html app.js workspace.js mfa.js style.css theme.css ./
RUN mkdir -p /dist \
    && cp index.html app.js workspace.js mfa.js style.css theme.css /dist/ \
    && printf '%s\n' "$APP_VERSION" > /dist/version.txt

FROM nginx:1.30.4-alpine@sha256:dc5069ad14f19660b141b21236140b91656bf89bbc3e2417c70ae650cd66104c AS runtime
RUN apk add --no-cache --upgrade libuuid=2.42.3-r1

COPY nginx.conf /etc/nginx/conf.d/default.conf
COPY nginx-main.conf /etc/nginx/nginx.conf
COPY --from=builder /dist/ /usr/share/nginx/html/

EXPOSE 80
USER 101:101
HEALTHCHECK --interval=15s --timeout=5s CMD wget -q -O /dev/null http://127.0.0.1/version.txt || exit 1
