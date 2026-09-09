FROM nginx:1.30.4-alpine@sha256:dc5069ad14f19660b141b21236140b91656bf89bbc3e2417c70ae650cd66104c
RUN apk add --no-cache --upgrade libuuid=2.42.3-r1
COPY nginx.conf /etc/nginx/nginx.conf
USER 101:101
EXPOSE 80
HEALTHCHECK --interval=15s --timeout=5s CMD wget -q -O /dev/null http://127.0.0.1/api/health || exit 1
