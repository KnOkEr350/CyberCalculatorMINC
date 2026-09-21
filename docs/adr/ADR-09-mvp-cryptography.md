# ADR-09: Криптографическое исполнение MVP

- Статус: `PROPOSED`
- Владелец решения: Security owner
- Backlog: Technical/Security
- Блокирует: CRYPTO-02–CRYPTO-11, OPS-10

## Контекст

ТЗ называет StandardAESCrypto и будущий CryptoPro adapter, но не фиксирует
полный wire format, KDF, параметры, управление ключами и требования к подписи.

## Варианты

1. AES-256-GCM + Argon2id для password-based пакетов.
2. AES-256-GCM + PBKDF2-HMAC-SHA256 при ограничениях зависимостей.
3. Отдельный envelope с provider-specific recipient keys.

## Критерий принятия

Опубликованы versioned format, KDF-параметры, nonce/salt semantics, AAD,
лимиты, zeroization policy, test vectors и политика совместимости версий.
