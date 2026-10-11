# Provenance

## How the code was written

Written for SigmaM2M, a oneM2M platform, in Go from the TALQ 2.7.1
OpenAPI definitions and specification (repository
github.com/TALQ-consortium/TALQ_specification, commit
b05f9a2c8df945fceab5f47c0ebc045724dc77e0; SHA-256 of the files:
48006a67... CMS API, 102cf1bd... gateway API, ac0e2b0d... data model).
No code was ported or copied from another implementation. Paths,
parameter names, header names, property names and status codes are facts
every implementation must use; the Go types, the client, the router and
the checks are our own design.

The client and the shapes were first part of SigmaM2M's TALQ adapter and
were cut out of it, with the same behaviour and error texts, on
2026-10-06. For the first release (2026-10-11) the module gained:

- operations_gen.go: the operation id, method, path and listed response
  codes of every operation of the gateway and CMS API files, written by
  a small script that reads the two JSON files (facts only; no
  description text). Regenerate it the same way for a new TALQ version.
- operations.go: `Do`, the typed helpers, `Serve`, `Match`,
  `CheckRequest`, `RequestBody`, `MTLSClient`, `CompatibleVersion`, our
  own code. The request rules it applies (protocol header and
  parameters, request IDs on writes, count answers in text/plain or
  JSON, gzip and deflate bodies with 415 for others, `Cache-Control:
  no-transform`, the lower minor version winning, mutual TLS, the nil
  UUID of a bootstrapping gateway) follow the specification's HTTP and
  security clauses, in our own words.
- An independent review, made before the release by a reviewer who read
  only the public definitions and this README, listed what the first
  README missed or misread (authentication is mutual TLS, not a bearer
  token; the 202 answers; counts; services, device classes, data
  packages, the DALI types; the HTTP rules). Those points are what the
  additions above cover, or what the README's "Not yet" lists.

## Sources

| Area (files) | Followed | Licence of the source | Taken |
|---|---|---|---|
| Operation tables (operations_gen.go) | TALQ 2.7.1 gateway and CMS API OpenAPI files | BSD-3-Clause | ids, methods, paths, status codes |
| Client, errors, paging, serving (client.go, operations.go) | the same files; specification clauses on addressing, HTTP use and security | BSD-3-Clause (OpenAPI), TALQ Consortium (specification) | facts only |
| Shapes and helpers (model.go) | TALQ 2.7.1 data model OpenAPI | BSD-3-Clause | facts only |

## Tests

| Test | Source |
|---|---|
| operations_test.go, example_test.go | our own, against HTTP stubs; the expected counts of operations are those of the definition files |
| talq-sim (separate module) | this client against the simulated gateway, and both against the definitions' schemas |

## Other implementations

None was found, in Go or otherwise (GitHub and pkg.go.dev, 2026-10-11).
