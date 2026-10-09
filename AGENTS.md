# Notes for AI coding agents

This repo is a test-engineering showcase: `service/` is a small Go brokerage
API (the system under test), `tests/` is a black-box Go test framework for it.

## Hard rules

- `tests/` never imports `service/`. They are separate Go modules on purpose.
- Tests go through the layers: test → `internal/service` → `internal/adapter`
  → `internal/httpclient`. Response bodies are decoded strictly by
  `internal/models` (unknown/missing fields and bad formats fail).
- Each test owns its data: own account, own symbol when prices move,
  `t.Parallel()` everywhere. No `time.Sleep`, no credentials in code.
- Do not change service code to make a test pass. A failing test is a finding:
  report it.

## Skills

- `.claude/skills/api-test-writer` — adding or extending API tests.
- `.claude/skills/incident-to-regression` — turning an incident into a
  failing-first regression test.

## Commands

```
make up          # stack: postgres + redpanda + broker
make test        # unit + API tests
make lint        # gofmt + vet
make down
```
