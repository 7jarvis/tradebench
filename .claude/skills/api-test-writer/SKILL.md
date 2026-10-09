---
name: api-test-writer
description: Write or extend black-box API tests in tests/api for this repo, in the repo's layered style (test -> service -> adapter -> httpclient, strict models). Use for new endpoint coverage, new positive/negative cases, contract or side-effect (Kafka) checks.
---

# Writing an API test in tradebench

Work as an SDET, not a code generator. Order of work: understand → plan →
design cases → implement → quality gates → self-review → report. Do not write
test code before the first three steps are done.

## 1. Understand before writing

Sources of truth, in order:
1. What the user gave you (ticket, test case, incident, curl).
2. `docs/openapi.yaml` — the contract: statuses, error codes, field formats.
3. Existing tests in `tests/api/` — the style to copy. Find the closest one first.
4. Service code in `service/` — only to understand behavior that the contract
   does not state. Never import it from tests.

If the expected behavior cannot be confirmed by any of these, stop and ask.
Do not invent business rules or error codes.

Check what is already covered before adding anything:
`grep -rn "<endpoint or code>" tests/api` — extend existing coverage, do not
duplicate it.

## 2. The layers (where code goes)

| Layer | Package | Knows | Add here when |
|---|---|---|---|
| test | `tests/api` | business scenario only | always |
| service | `tests/internal/service` | domain actions, maps responses to models / `*APIError` | a new business action |
| adapter | `tests/internal/adapter` | method, path, headers, serialization | a new endpoint |
| httpclient | `tests/internal/httpclient` | transport | almost never |
| models | `tests/internal/models` | contract: fields, formats, invariants (`Validate`) | a new request/response shape or field |
| fixtures | `tests/internal/fixtures` | isolated test data + cleanup | a reusable precondition |
| builders | `tests/internal/builders` | valid default payloads | a new kind of request |
| check | `tests/internal/check` | assertions | a genuinely new assertion type |

Rules:
- Tests never touch HTTP, URLs, status-code parsing or JSON. If a test needs
  that, a lower layer is missing a method.
- A new response field goes into the model AND its `Validate()` (format/enum),
  otherwise the strict decoder will reject the response.
- Reuse before adding. New code is minimal and in the style of its neighbors.

## 3. Plan (write this out before code)

```
Scenario:        ...
Contract source: openapi path/op, error code(s)
Cases:           implement now: ... | later: ... | not covering: ... (why)
Reuse:           fixtures ..., builders ..., service methods ...
Add:             ... (or "nothing")
Assertions:      response fields, follow-up state (GET), side effects (events)
Risks:           shared state? timing? cleanup?
```

Case checklist for an endpoint: happy path; each validation field incl.
boundaries (min, max, max+1); auth; not found / other account's resource;
state conflicts (cancel filled, closed account); idempotency
(`client_order_id`); concurrency if it moves money; side effects on
`trade-updates`. Pick what the request needs; say what you skipped.

## 4. Implementation rules

- File per area: `accounts_test.go`, `orders_test.go`, `limit_orders_test.go`,
  `positions_test.go`, `auth_test.go`, `contract_test.go`, `concurrency_test.go`,
  `events_test.go`.
- Name: `Test<Subject>_<Condition>_<Outcome>`, e.g.
  `TestCancelOrder_NotOpen_NotCancelable`. Never `TestOrder2`, `TestNegative`.
- Every test starts with `t.Parallel()`.
- Isolation: own account (`fixtures.Account` / `FundedAccount`); own symbol
  (`fixtures.Symbol`) if the test reads or moves a price. Never move the
  price of a seeded symbol (AAPL, MSFT, NVDA, TSLA).
- Cleanup is automatic via fixtures. Anything created outside fixtures needs
  `t.Cleanup` with `context.Background()` (t.Context() is canceled by then).
- Arrange / act / assert separated by blank lines.
- Expected money values are computed with `money.Of(t, "...").Mul/Add/Sub`
  from the inputs, not hard-coded results.
- Preconditions: `check.NoError(t, err, "what was being done")` (stops).
  Field checks: `check.Equal(t, "field name", got, want)` (soft; all mismatches reported).
- Expected rejections: `check.APIError(t, err, http.StatusX, "code")` or
  `check.ValidationError(t, err, "field")`. After a rejection, assert there
  was no side effect (balance unchanged, no order created).
- Table tests for validation: map of case name → input; `t.Run` + `t.Parallel()`.
- Kafka side effects: create `events.Listen(t)` BEFORE the action, then
  `listener.Expect(acc.ID, "new", "fill")`.
- No `time.Sleep` in tests. Wait on a condition with a deadline.
- No tokens or credentials in code; config comes from env (`internal/config`).
- Raw/malformed payloads only through `PlaceOrderRawJSON`-style service
  methods, and only in `contract_test.go`.

## 5. Quality gates (run them)

```
make lint                                   # gofmt + vet, both modules
cd tests && go test ./internal/...          # framework self-tests
cd tests && go test -race -count=1 -run '<NewTest>' ./api/...   # new test alone
cd tests && go test -race -count=3 -run '<NewTest>' ./api/...   # flakiness check
make test-api                               # full suite once, stack must be up
```

If the stack is not running (`make up`) or Kafka is unavailable, say so in the
report; event tests skip without `KAFKA_BROKERS`.

Prove the test can fail: break the expectation (or the service behavior)
locally, confirm the failure message is clear, revert.

## 6. Self-review before answering

- Does the test verify the stated scenario, including follow-up state?
- Is it independent of other tests and of execution order?
- Is every new model field validated?
- Any duplication of an existing test, helper or builder?
- Any edits outside the task? Revert them.
- Would a reviewer understand the failure message without reading the code?

## 7. Report

```
Covered:        ...
Source:         openapi path / ticket / incident
Cases:          implemented ... | suggested ... | not covered ... (why)
Files:          ...
Reused / added: ...
Gates:          command -> result (or why not run)
Open questions: ...
```

Hard stops: tests against production; changing service code to make a test
pass; disabling checks, adding skip/sleep to hide flakiness; deleting tests.
