# talq

The TALQ Smart City Protocol 2.x (2.7.1) for a central management system (CMS), in pure Go; not affiliated with the TALQ Consortium.

[![Go Reference](https://pkg.go.dev/badge/github.com/cB-Sigma/talq.svg)](https://pkg.go.dev/github.com/cB-Sigma/talq) [![test](https://github.com/cB-Sigma/talq/actions/workflows/test.yml/badge.svg)](https://github.com/cB-Sigma/talq/actions/workflows/test.yml) [![Licence: Apache-2.0](https://img.shields.io/badge/licence-Apache--2.0-blue.svg)](LICENSE) [![Go](https://img.shields.io/badge/go-1.22-00ADD8.svg)](go.mod)

A client that can run every operation of a TALQ gateway's API (typed
helpers for the common ones, a generic `Do` by operationId for the
rest), with the protocol's headers and parameters, paging, counts, 202 answers and TALQ
error answers; the request checks and a router for serving either side
of the protocol (a CMS API for gateways, or a gateway API); and the
shapes and helpers both sides share. The operation tables come from the
TALQ consortium's public OpenAPI definitions. Standard library only, no
cgo. It was written for SigmaM2M, a oneM2M platform, whose TALQ adapter
is built on it.

## Features

| Feature | Status |
|---|---|
| Every operation of the 2.7.1 gateway API (129) and CMS API (98) as data: operationId, method, path template, listed status codes (`GatewayOperations`, `CMSOperations`, `FindOperation`, `Operation.Expand`, `Operation.Params`) | yes |
| Running any operation by its operationId with path parameters (escaped), extra query parameters, a JSON body and a JSON answer; 202 visible in `Response.Status` (`Client.Do`) | yes |
| Protocol parameters on every request: `talq-api-version` header, `clientAddress`, a fresh `talqRequestId` UUID on every write, `talqOriginRequestId` when the caller passes one, `Cache-Control: no-transform` | yes |
| Count operations answering a bare integer in text/plain or JSON (`Client.Count`, `CountOf`, `CountDevices`) | yes |
| Devices: whole list, one page (`DevicesPage` with the four `pagination-*` headers in `Page`), every page (`ListDevices`), a gateway that ignores paging, the `deviceClasses` filter, one device, functions with `attributeScope`, attributes, PATCH of devices, functions and attributes | yes |
| Override commands (`Override`, `OverrideTarget`, `LevelCommand`) and assign commands (`Assign`) | yes |
| Collections with list, add, bulk update, count, get, update and delete: groups (and members), calendars, control programs, lamp, luminaire, driver, controller and bracket types, the six DALI types of 2.7 (`Collections`, `List`, `Add`, `UpdateAll`, `Update`, `Get`, `Delete`, `CountOf`) | yes |
| Logger configurations, pulled log reports, services, device classes, data packages, assign command lists | yes, through `Do` |
| TALQ error answers read with key, description and references (`APIError`, `ErrorMessage`, `ErrorReference`), the recommended keys as constants, refusals told from outages (`Refused`, `Unsupported`) | yes |
| Redirects: followed as Go's `http.Client` does (307 and 308 keep the method and body); a write that a 301, 302 or 303 turned into a GET is an error saying nothing was written; with a `CheckRedirect` that stops, any 3xx is an error naming the new location | yes |
| Request bodies with an explicit Content-Length (never chunked) | yes |
| Serving either API: routing by the operation tables (literal segments before parameters), 404 and 405, the request checks of every TALQ request including 411 for chunked bodies (`CheckRequest`), gzip and deflate request bodies with 415 for others (`RequestBody`), JSON checks, error bodies (`Serve`, `Match`, `WriteError`) | yes |
| Bootstrap rules a CMS applies: a registering gateway must send the nil address (`CheckGatewayRegistration`, `NilUUID`), 403 while a bootstrap is unfinished (`WriteBootstrapping`) | yes; the bootstrap state is the application's |
| Mutual TLS between CMS and gateway (`MTLSClient`); bearer token and HTTP basic as vendor extras | yes |
| Versions: the side with the higher minor version adapts down; different major versions are not compatible (`Compatible`; `CompatibleVersion` picks the lower without the major check; `MajorMinor`) | yes |

## Choosing a TALQ library

No other open TALQ implementation was found (GitHub search for "talq"
and for the OpenAPI operation names, 2026-10-11): the consortium's
repository holds the specification and the OpenAPI files, not code.
Generating a client from those files with an OpenAPI generator is the
alternative; it gives typed models for all 252 data model schemas
(this module leaves bodies to `map[string]any` or your own types) but
not the protocol rules (request IDs, paging across gateways that ignore
it, 202 handling, the request checks a server must make, version
negotiation), which this module adds.

## Install

```
go get github.com/cB-Sigma/talq@latest
```

Requires Go 1.22 or later.

## Quick start

```go
import "github.com/cB-Sigma/talq"

c := &talq.Client{URL: "https://gw.example/talq", CMSAddress: cmsUUID,
	HTTP: talq.MTLSClient(cmsCert, gatewayRoots)}
devs, err := c.ListDevices(ctx, nil, 100) // pages when the gateway pages
n, err := c.CountDevices(ctx, []string{"streetlightController"})
tc, _ := talq.OverrideTarget(40, "", nil, "ref-1")
resp, err := c.Override(ctx, []talq.Ref{{Resource: "devices", Address: devs[0].Address}}, tc)
// resp.Status is 201, or 202 when the gateway reports the result later
var cals []map[string]any
err = c.List(ctx, "calendars", &cals)
_, err = c.Do(ctx, "getLogReport", map[string]string{"loggerAddress": logger}, nil, nil, &report)
```

Serving the CMS API to gateways:

```go
h := talq.Serve("/talq", talq.CMSOperations, func(w http.ResponseWriter, r *http.Request,
	op talq.Operation, params map[string]string, body []byte) {
	switch op.ID {
	case "sendLogReport": // ...
	}
})
```

`Example` in example_test.go runs; [talq-sim](https://github.com/cB-Sigma/talq-sim)
runs the client against a simulated gateway.

## Documentation

API reference: https://pkg.go.dev/github.com/cB-Sigma/talq

| Package | What it does |
|---|---|
| `talq` (module root) | `Client` (`Do`, `Count`, `Call`, `CallQ`, `ListDevices`, `DevicesPage`, `CountDevices`, `GetDevice`, `GetFunction`, `GetAttribute`, `PatchDevice`, `PatchFunction`, `PatchAttribute`, `Override`, `Assign`, `List`, `Add`, `UpdateAll`, `Update`, `Get`, `Delete`, `CountOf`), `Operation`, `GatewayOperations`, `CMSOperations`, `FindOperation`, `Match`, `Serve`, `CheckRequest`, `RequestBody`, `WriteError`, `APIError`, `ErrorMessage`, `Refused`, `Unsupported`, `Page`, `PageOf`, `MTLSClient`, `Compatible`, `CompatibleVersion`, `CheckGatewayRegistration`, `WriteBootstrapping`, `NilUUID`; shapes and helpers (`Device`, `Ref`, `AttrValue`, `LevelCommand`, `OverrideTarget`, `SourceDevice`, `IsUUID`, `NewRequestID`, `AssetKinds`, `AssetAttrs`, `GroupPurposes`, `Collections`) |

Changes per version are in [CHANGELOG.md](CHANGELOG.md).

## Testing

```
go test -race ./...
```

The unit tests check the operation tables (every path template routes
back to its operation, with reserved characters in parameters), the
parameters and headers the client sends, counts, paging with and
without a paging gateway, error answers, the collection methods and the
checks `Serve` makes, against small HTTP stubs. The tests against the
simulated gateway are in talq-sim.

### Interop tested against

- No TALQ gateway or CMS of another maker: none is open, and TALQ
  certification needs consortium membership. The tests run this client
  against [talq-sim](https://github.com/cB-Sigma/talq-sim), written by
  the same author from the same definitions, so that agreement is not
  independent.
- Outside evidence: the TALQ consortium's own OpenAPI files for 2.7.1
  (github.com/TALQ-consortium/TALQ_specification, commit b05f9a2,
  BSD-3-Clause). The operation tables are taken from them, and
  talq-sim's `interop` module validates every request body this client
  sent in a run of all 129 gateway operations, every answer the
  simulator gave, its error bodies and what it sends to a CMS against
  the schemas of those files (161 and 10 bodies on 2026-10-11, all
  passed after two simulator fixes listed in talq-sim's changelog).
- An independent review of this README against the public definitions,
  made before the release by a reviewer who did not read the code; its
  findings (mutual TLS rather than a bearer token, 202 answers, counts,
  the HTTP rules, bootstrap rules, redirects, version majors) were
  fixed or are listed under Not yet.

## Not yet

| What | Why |
|---|---|
| Typed Go structs for the data model's 252 schemas | bodies are `map[string]any` or the caller's own types; the schemas are large and change between minor versions |
| A complete CMS server (bootstrap state machine: assigning gateway addresses, 403 while a gateway bootstraps, resync) | it holds an application's state; `Serve` and `CheckRequest` are the protocol part, the rest is SigmaM2M's |
| Data package downloads with Range, 206 and 416 (CMS API `getDataPackage`: the gateway downloads from the CMS) | not needed by SigmaM2M yet; `Do` runs the operation, without range handling |
| Compressing request bodies | optional in TALQ; servers built with `Serve` accept gzip and deflate |
| Answering in the request's Content-Encoding | `Serve` decodes compressed requests, but handlers answer uncompressed; wrap the ResponseWriter if a peer needs it |
| Retries with the CMS retry period and count a gateway publishes (`cmsRetryPeriod`, `cmsNumberOfRetries`) | the client makes one attempt; retry policy is the caller's |
| A run against a certified TALQ gateway or CMS | none is openly available; certification needs consortium membership |

## Related repositories

- [talq-sim](https://github.com/cB-Sigma/talq-sim): a simulated TALQ
  gateway answering every gateway operation, with faults, as a Go
  package and a command.

## Status

v0.x: the API may change before v1.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md).

## Security

See [SECURITY.md](SECURITY.md) to report a vulnerability privately.

## Licence

Apache-2.0 (see [LICENSE](LICENSE) and [NOTICE](NOTICE)). Copyright 2026
Abhinav Gautam. TALQ is a trademark of the TALQ Consortium; the name is
used only to say which protocol this module implements.
