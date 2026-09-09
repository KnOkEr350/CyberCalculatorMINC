FROM nginx:1.31.5-alpine@sha256:72ba65eb42c10344912a84ff42408db7d34f2feb642204570ab8fc5ffd29f1d3
RUN apk add --no-cache --upgrade libuuid=2.42.3-r1
COPY nginx.conf /etc/nginx/nginx.conf
USER 101:101
EXPOSE 80
HEALTHCHECK --interval=15s --timeout=5s CMD wget -q -O /dev/null http://127.0.0.1/api/health || exit 1
