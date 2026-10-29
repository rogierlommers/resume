---
title: Password-backed login uses opaque server-side sessions
date: 2026-10-29
last_updated: 2026-10-29
category: best-practices
module: visits authentication
problem_type: best_practice
component: authentication
severity: high
applies_when:
  - Changing authentication for the visits dashboard
  - Adding session cookies backed by VISITS_TOKEN
tags: [authentication, sessions, cookies, password-security]
---

# Password-backed login uses opaque server-side sessions

## Context

`VISITS_TOKEN` is a human-entered password as well as an environment secret. Deriving a stateless signed-session key directly from that value would let anyone who obtains a cookie test password guesses offline against its signature. A stolen cookie should grant no more than its bounded session lifetime.

The dashboard requires both `VISITS_USERNAME` and `VISITS_TOKEN`. Compare hashes of both submitted values in constant time and return the same error for every mismatch so the response does not reveal which credential was correct.

## Guidance

Create session identifiers with `crypto/rand`, return the opaque identifier in the cookie, and retain only its SHA-256 hash in server memory. Validate the cookie through that server-side map rather than through a password-derived signature.

The visits session has a 12-hour absolute expiry, is removed on sign-out, is lost on restart, and uses `HttpOnly` and `SameSite=Strict`. It receives `Secure` when Go handles TLS directly or the reverse proxy supplies `X-Forwarded-Proto: https`. Session storage is capped so repeated successful logins cannot grow memory without a bound.

## Why This Matters

An opaque random identifier contains no verifier for the configured password. Stealing one still requires incident response, but it does not create an offline password oracle and it stops working at expiry, sign-out, server restart, or eviction. Keeping only a hash server-side also avoids retaining usable cookie values in process state.

## When to Apply

- When changing cookie generation or session validation.
- When considering stateless sessions derived from `VISITS_TOKEN`.
- When moving sessions to shared persistence for multiple server replicas.

## Examples

Authentication changes should continue to test a wrong username, a wrong password, missing configuration, successful login, cookie attributes, absolute expiry, sign-out invalidation, and the session-count bound. Production deployment must preserve HTTPS signaling from the reverse proxy so the browser receives a `Secure` cookie.

## Related

- `src/visits.go` defines visits authentication and session lifecycle behavior.
- `src/visits_test.go` covers the allowed and denied authentication paths.
- `readme.md` documents the HTTPS and reverse-proxy requirement.
