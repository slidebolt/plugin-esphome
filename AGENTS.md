# plugin-esphome Instructions

`plugin-esphome` follows the reference runnable-module architecture.

- Keep `cmd/plugin-esphome/main.go` as a thin wrapper only.
- Put runtime lifecycle and device wiring in `app/`.
- Keep protocol/private helpers under `internal/...`.
- Prefer testing `app/` and `internal/...`; keep `cmd` focused on the BDD and legacy compatibility harness only.
- Keep plugin-local test env at the module root in `.env.local`, not under `cmd/...`.
