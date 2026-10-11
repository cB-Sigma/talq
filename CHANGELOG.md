# Changelog

All notable changes to this module. Versions follow semantic versioning;
before v1 the API may change in minor versions.

## v0.1.0 (2026-10-11)

First release as a module of its own, split from SigmaM2M.

- The client for a gateway's API: `Client` with `Call`, `CallQ` and
  `ListDevices` as SigmaM2M used them, and new: `Do` for any operation
  by operationId, `Count`, `DevicesPage` and `Page` (the
  `pagination-*` headers), `CountDevices`, device, function and
  attribute reads and PATCHes, `Override`, `Assign`, and the collection
  methods `List`, `Add`, `UpdateAll`, `Update`, `Get`, `Delete` and
  `CountOf` for groups, calendars, control programs, asset types and
  the DALI types of 2.7.
- `GatewayOperations` and `CMSOperations`: every operation of the TALQ
  2.7.1 OpenAPI definitions (129 and 98) as data.
- Serving: `Serve`, `Match`, `CheckRequest`, `RequestBody` (gzip and
  deflate bodies, 415 for others), `WriteError`.
- `ErrorMessage.References`, the recommended error keys as constants.
- `MTLSClient`, `Compatible`, `CompatibleVersion`, `NilUUID`,
  `CheckGatewayRegistration`, `WriteBootstrapping`; `CheckRequest`
  answers 411 for chunked bodies.
- A write that a redirect turned into a GET is now an error (before,
  Go's client followed a 301 and the GET's 200 looked like success).
- Requests carry `Cache-Control: no-transform`.
- The simulated gateway lives in its own module,
  [talq-sim](https://github.com/cB-Sigma/talq-sim), with the tests that
  drive it.
