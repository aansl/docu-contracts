# docu-contracts

Language-neutral, single source of truth for the APIs that docu services
expose to each other. `contracts/` holds hand-written OpenAPI specs; the
per-language packages under `packages/` are **generated** from those specs
and committed so consumers never need codegen tooling installed.

Replaces the former `docu-proto` repo (buf + protobuf/gRPC, Go-only).

```
contracts/               # OpenAPI specs — the source of truth, edit these
  auth/v1/openapi.yaml   # docu-auth internal API (session validation)
  llm/v1/openapi.yaml    # docu-llm internal API (tier-routed completions)
  waitlist/v1/openapi.yaml # docu-waitlist internal product-question intake
packages/
  go/                    # self-contained Go module
    httpx/               # shared internal client: timeouts, retries, X-Internal-Token
    generated/authv1/    # oapi-codegen client for contracts/auth/v1
    generated/llmv1/     # oapi-codegen client for contracts/llm/v1
    generated/waitlistv1/ # oapi-codegen client for contracts/waitlist/v1
```

## Conventions for internal APIs

- Internal endpoints live on a **dedicated port** of the service
  (`PORT_INTERNAL`, e.g. `:4001`), never on the public router — the edge
  gateway cannot expose what isn't mounted on the public port.
- Every request carries the shared `X-Internal-Token` header (skipped in
  dev when unset).
- Secrets like session tokens travel in headers, never query strings.
- Callers cache positive results briefly and fail closed on errors.
- Wire format is JSON with snake_case field names.

## Consuming from Go

```sh
go get github.com/aansl/docu-contracts/packages/go
```

```go
import (
	"github.com/aansl/docu-contracts/packages/go/generated/authv1"
	"github.com/aansl/docu-contracts/packages/go/httpx"
)

client, err := authv1.NewClient(
	"http://localhost:4001", // docu-auth internal URL
	authv1.WithHTTPClient(httpx.New(httpx.Options{
		Timeout:       5 * time.Second,
		MaxRetries:    2,
		InternalToken: internalToken,
	})),
)

resp, err := client.GetSessionUserWithResponse(ctx, authv1.GetSessionUserParams{
	XSessionToken: sessionToken,
})
```

Until the repo is published, services can develop against a local checkout:

```
replace github.com/aansl/docu-contracts/packages/go => ../docu-contracts/packages/go
```

(Remove the `replace` before building images or tagging releases — CI
cannot resolve local paths.)

## Consuming from TypeScript (or any other language)

TS codegen is scaffolded but not generated yet (no consumer). When needed:

```sh
bunx openapi-typescript contracts/auth/v1/openapi.yaml -o packages/ts/src/auth/v1/index.ts
```

Any other language: point its OpenAPI generator at the YAML in
`contracts/` — nothing in this repo is Go-specific except `packages/go`.

## Adding or changing a contract

1. Edit (or add) the spec under `contracts/<service>/vN/openapi.yaml`.
2. Run `make generate-go && make tidy build test` — extend
   `generate-go` with a line for any new contract.
3. Non-additive changes to a merged spec need a version bump
   (`v2`); CI runs `oasdiff` on PRs and fails on breaking changes.
4. Commit specs + regenerated packages together, then `make release-patch`
   (or `-minor`/`-major`) to tag.

## CI

On PRs: spectral lint, breaking-change check, Go build/test, and a guard
that committed generated code matches the specs. On pushes to main:
regenerates and auto-commits any drift.
