FROM postgres:16-alpine@sha256:cf78e76683b9ca8c5733cbbdce6c9262b45b6767934dd0a95e671f9a0fc20685
RUN apk add --no-cache --upgrade libuuid=2.42.3-r1 libcrypto3=3.5.8-r0 libssl3=3.5.8-r0 \
    && rm /usr/local/bin/gosu
# Existing Alpine PostgreSQL volumes already belong to UID 70. No root or
# privilege-switching helper is needed for either initialization or normal use.
USER postgres:postgres
HEALTHCHECK --interval=10s --timeout=5s CMD pg_isready -U "$POSTGRES_USER" -d "$POSTGRES_DB" || exit 1
