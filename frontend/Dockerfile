FROM alpine:3.20 AS builder

ARG APP_VERSION=dev
WORKDIR /src
COPY index.html app.js style.css theme.css ./
RUN mkdir -p /dist \
    && cp index.html app.js style.css theme.css /dist/ \
    && printf '%s\n' "$APP_VERSION" > /dist/version.txt

FROM nginx:1.27-alpine AS runtime

COPY nginx.conf /etc/nginx/conf.d/default.conf
COPY --from=builder /dist/ /usr/share/nginx/html/

EXPOSE 80
