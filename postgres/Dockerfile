FROM postgres:18-alpine@sha256:d3e1620b530c944afa6e887d22eb899824da68e19c52024bf98f5220c88a65b2
RUN apk add --no-cache --upgrade libuuid=2.42.3-r1 libcrypto3=3.5.8-r0 libssl3=3.5.8-r0 \
    && rm /usr/local/bin/gosu
# Existing Alpine PostgreSQL volumes already belong to UID 70. No root or
# privilege-switching helper is needed for either initialization or normal use.
USER postgres:postgres
HEALTHCHECK --interval=10s --timeout=5s CMD pg_isready -U "$POSTGRES_USER" -d "$POSTGRES_DB" || exit 1
