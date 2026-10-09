---
name: incident-to-regression
description: Turn a production incident, bug report or postmortem into a failing-first regression test in tests/api. Use when the user pastes an incident, a bug ticket, logs or a request id and wants it covered so it cannot recur.
---

# Incident → regression test

Goal: one small, deterministic test that fails on the buggy behavior, passes
on the fix, and stays in the suite. Uses the `api-test-writer` conventions
for everything not covered here.

## 1. Extract the facts (no code yet)

From the incident, write down:

```
Trigger:     the exact request sequence / state that caused it
Expected:    what the contract (docs/openapi.yaml) says should happen
Actual:      what happened (status, body, balance, event, log line)
Invariant:   the rule that was broken, stated generally
             e.g. "cash never goes below zero", "a canceled order never fills",
                  "each order state change emits exactly one event"
Scope:       one account? concurrency? a price move? Kafka only?
```

If any of Trigger / Expected is unknown, ask. Logs and request ids help: the
service logs every request with `request_id`; tests send `X-Request-Id: qa-…`.

## 2. Choose the test level

- Reproducible with a request sequence → API test in the matching file.
- Needs simultaneity (double spend, duplicate submit) → `concurrency_test.go`,
  use `runConcurrently`; assert the invariant over the totals, not per call.
- Event missing / duplicated / out of order → `events_test.go` with
  `events.Listen` before the action.
- Pure calculation (rounding, parsing) → also add a unit test in `service/`.

## 3. Write it failing-first

1. Write the test against the expected behavior.
2. Run it against the buggy build (or reproduce the bug by temporarily
   reverting the fix) and confirm it FAILS for the right reason. Paste the
   failure output into the report.
3. Run it against the fixed build: passes. Then `-count=10 -race` for stability.

A regression test that has never been seen failing proves nothing.

## 4. Name and document

- Name states the invariant, not the ticket: `TestCancelOrder_Twice_ReleasesReservationOnce`.
- One comment line above the test: `// Regression: <incident id> — <one-line summary>.`
- Assert the invariant AND the specific symptom from the incident.

## 5. Report

```
Incident:   id + one line
Invariant:  ...
Test:       file:TestName
Failing run (before fix): <output excerpt>
Passing run (after fix):  go test -race -count=10 ... -> ok
Related gaps found: ... (other places the same invariant is unguarded)
```
