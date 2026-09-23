module cybercalc

go 1.27.1

// Runtime-зависимости ограничены драйвером PostgreSQL и потоковым zstd-кодеком
// для CAS (STORE-03). ORM, роутеры и web-фреймворки не используются.
require (
	github.com/klauspost/compress v1.20.0
	github.com/lib/pq v1.10.9
)
