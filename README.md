# tradebench

A Go test framework for a brokerage API, together with the small Go service it
tests.

My production test code lives in private company repositories under NDA, so
this repository is a compact, runnable demo of how I build API test
automation: the framework architecture, test design, contract and side-effect
checks, CI, and the way I use AI agents in the process.

```
service/   Go sandbox broker: accounts, orders, positions, prices (system under test)
tests/     Go black-box test framework + API test suite (separate Go module)
deploy/    Helm chart for the service
load/      k6 load test
docs/      OpenAPI contract
.claude/   agent skills used to write and maintain tests
```

## Quick start

Requires Docker and Go 1.24+.

```bash
make up      # postgres + redpanda + broker, waits until healthy
make test    # unit tests + 79 API test cases (incl. Kafka event checks)
make down
```

Other targets: `make lint`, `make load` (k6), `make profile` (pprof), `make logs`.

## The system under test

A deliberately small brokerage domain with real-world sharp edges:

- **Accounts** with cash and *buying power* (cash minus what open buy orders reserve).
- **Market and limit orders**: market and marketable limit orders fill at once
  at the current price; others rest and reserve buying power until a price
  move crosses the limit (`PUT /v1/market/prices/{symbol}` is the sandbox
  "market").
- **Positions** with quantity-weighted average entry price and unrealized P/L.
- **Trade-update events** to Kafka (Redpanda) on every order state change,
  written through a **transactional outbox**, so an event is never lost and
  never emitted for a rolled-back change.
- **Concurrency safety**: all money movement happens under a row lock with a
  fixed lock order, so concurrent orders cannot double-spend.
- Money as integer cents on the inside and two-decimal strings on the wire.

The full contract is in [`docs/openapi.yaml`](docs/openapi.yaml).

## Framework architecture

Four layers, each with one responsibility. A test talks only to the top layer
and knows nothing about HTTP.

```mermaid
flowchart LR
  T["tests/api<br/>business scenarios"] --> S["service<br/>domain actions,<br/>typed results / APIError"]
  S --> A["adapter<br/>endpoints, headers,<br/>serialization"]
  A --> H["httpclient<br/>transport + traffic log"]
  S -. decodes with .-> M["models<br/>strict contract:<br/>fields, formats, invariants"]
  T -. data .-> F["fixtures / builders<br/>isolated data + cleanup"]
  H -->|HTTP| SUT[(broker service)]
  SUT -->|outbox| K[(Redpanda)]
  T -. events.Listen .-> K
```

| Layer | Responsibility | Changes when |
|---|---|---|
| `httpclient` | Send a request, return status/headers/body. Logs every request and response into the test's own log, tagged with an `X-Request-Id` that also appears in service logs. | transport changes (only this file) |
| `adapter` | Knows the API surface: method, path, auth header, JSON serialization. No judgement about responses. | an endpoint is added or moved |
| `service` | Business actions in domain language (`PlaceOrder`, `Deposit`, `SetPrice`). Turns responses into typed models; a documented 4xx becomes `*APIError`, anything else (5xx, wrong status, broken body) is a plain error, so a server bug is never mistaken for an expected rejection. | a new business action |
| `models` | The contract. Decoding fails on unknown fields, missing fields, wrong types and invalid values (money format, enums, order state invariants such as "filled ⇒ filled_at set"). | the contract changes |

A test reads like the scenario it checks:

```go
func TestLimitBuy_FillsWhenPriceDropsToLimit(t *testing.T) {
	t.Parallel()
	broker := fixtures.Broker(t)
	acc := fixtures.FundedAccount(t, broker, "1000.00")
	sym := fixtures.Symbol(t, broker, "100.00")
	order, err := broker.PlaceOrder(t.Context(), acc.ID, builders.LimitBuy(sym, 5, "90.00"))
	check.NoError(t, err, "place resting limit buy")

	_, err = broker.SetPrice(t.Context(), sym, "89.50")
	check.NoError(t, err, "move price through limit")

	got, err := broker.GetOrder(t.Context(), acc.ID, order.ID)
	check.NoError(t, err, "get order")
	check.Equal(t, "status", got.Status, "filled")
	...
}
```

When it fails, the output contains the full request/response exchange of that
test only, with request ids to find the matching service log lines.

## Test design

| Area | What is covered |
|---|---|
| Accounts | open, trim/validate owner name (boundaries incl. unicode), deposit validation (0, negative, 3 decimals, exponent, 0.01, max, max+0.01), closed-account rules, close is idempotent and cancels open orders |
| Market orders | fill price, cash and position effects, exact-buying-power boundary, insufficient buying power with **no side effects**, partial and full sells, selling more than held |
| Limit orders | rest vs immediate fill with price improvement, reservation of buying power, fill on price move (and no fill one cent above the limit), cancel releases reservation exactly once, canceled order never fills later, open sells reduce sellable quantity |
| Positions | weighted average entry price, cost basis, market value, negative unrealized P/L, empty list is `[]` not `null` |
| Validation | every order field incl. boundaries; `client_order_id` max length and uniqueness per account |
| Security | missing / wrong / near-miss tokens, probes open without a token, another account's order is invisible (404) |
| Contract | numbers instead of strings, misspelled and unknown fields, empty, truncated, array or concatenated bodies → 400 and nothing created |
| Concurrency | 20 simultaneous buys with money for 5 → exactly 5 fills, cash exactly 0; 10 simultaneous duplicates of one `client_order_id` → exactly one order |
| Events (Kafka) | `new → fill` and `new → canceled` sequences per account, fill price/qty, event payload equals the API order, rejected orders publish nothing |

Principles behind it:

- **Black box.** `tests/` is a separate Go module and cannot import service
  code. Expected values are computed by the test's own `money` helper, not by
  reusing the code under test.
- **Isolation instead of ordering.** Each test creates its own account and,
  if it moves prices, its own symbol. Everything runs with `t.Parallel()`.
  Cleanup is registered by fixtures.
- **Assert the state, not just the response.** After an action the tests
  re-read balances, orders and positions; after a rejection they verify that
  nothing changed.
- **No sleeps.** Readiness and Kafka events are awaited with deadlines.
- **The framework is tested too.** `tests/internal/models` has self-tests that
  feed broken payloads to the decoder. The concurrency test was checked by
  mutation: removing the row lock in the service makes it fail with 18 fills
  instead of 5.

## CI

`.github/workflows/ci.yml`:

1. **lint + unit**: `gofmt`, `go vet`, service unit tests and framework
   self-tests with `-race`.
2. **image**: builds the distroless, non-root image and scans it with Trivy
   (fails on fixable HIGH/CRITICAL).
3. **helm**: `helm lint --strict` and a render with all optional resources.
4. **API tests**: `docker compose up --wait`, full suite with `-race` via
   gotestsum, JUnit report published to the run, service logs attached on
   failure.
5. **load** (manual trigger): k6 against the same stack.

## Deployment

`deploy/helm/broker` is a production-shaped chart: non-root, read-only root
filesystem, all capabilities dropped, `seccompProfile: RuntimeDefault`, no
service-account token mounted, secrets referenced from an existing Secret
(never rendered), startup/liveness/readiness probes, zero-downtime rolling
update, PodDisruptionBudget, topology spread, optional HPA and NetworkPolicy.
The container healthcheck is a subcommand of the binary, since distroless has
no shell or curl.

## Load and profiling

`load/orders.js` runs an open-model (constant arrival rate) order flow with
latency thresholds per endpoint (p95/p99) and an error-rate budget. While it
runs, `make profile` opens a 30-second CPU profile from the service's pprof
endpoint (bound to loopback only).

## Working with AI agents

The skills in `.claude/skills` are the instructions I give an agent for this
repo:

- `api-test-writer`: analyze the contract and existing tests first, write a
  plan and test design, reuse the layers, run quality gates (including
  proving the new test can fail), self-review, report.
- `incident-to-regression`: turn an incident into a failing-first regression
  test that guards the broken invariant.

How I use them: the agent drafts, I review every change. Where it helps most:
enumerating cases from a contract, boilerplate inside an established
architecture, turning an incident description into a reproducible scenario.
Where it does not replace judgement: deciding what the expected behavior
actually is, concurrency invariants, and whether a flaky test is a test
problem or a product bug.

## Roadmap

- gRPC surface for orders and `ghz` load profile.
- Resilience experiments: stop Redpanda during an order flow and assert the
  outbox catches up with no lost or duplicated events; latency injection with
  Toxiproxy / Chaos Mesh.
- Testcontainers-go for running the stack from `go test` directly.
