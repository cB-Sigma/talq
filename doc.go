// Copyright 2026 Abhinav Gautam
// SPDX-License-Identifier: Apache-2.0

// Package talq is the TALQ Smart City Protocol 2.x (2.7.1) for a Central
// Management Software (CMS): a client for every operation of a gateway's
// API (GatewayOperations, run by Client.Do, with typed helpers for
// devices, paging, counts, functions and attributes, override and assign
// commands and every collection), the protocol's request checks and a
// router for serving either API (CheckRequest, Serve, with
// CMSOperations for a CMS), TALQ error answers in both directions, and
// the shapes and small helpers both sides share (devices, resource
// references, attribute values, level and override commands, versions,
// mutual TLS).
//
// It follows the public TALQ OpenAPI definitions (BSD-3-Clause); the
// operation tables are facts taken from them, and no specification text
// is copied here.
package talq
