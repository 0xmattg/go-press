// Copyright (c) 2026 0xmattg and GoPress contributors
// SPDX-License-Identifier: MIT

// Package user contains GoPress accounts, authentication, and authorization.
//
// It covers three concerns that share one User table:
//
//   - Admin authentication: Auth issues HMAC-signed JWTs for the admin cookie
//     and, through ActiveClaims, re-reads the account on every request so
//     disabled users and role changes take effect before the token expires.
//     Passwords are stored as bcrypt hashes.
//   - Authorization: RBAC maps roles to "resource.action" capabilities. It
//     answers capability questions only; ownership for *_own actions is
//     checked by the caller against the resource.
//   - Public-site sign-in: plugins register identity providers in
//     ProviderRegistry and hand a protocol-neutral VerifiedIdentity to
//     PublicAuth. IdentityBroker applies RegistrationPolicy to find, link, or
//     provision the local account, and SessionManager issues revocable
//     server-side sessions that store only token digests.
//
// Provider plugins verify protocols (OIDC, wallet signatures, and so on) but
// never decide roles, account creation, or linking; those rules live here so
// every provider behaves the same.
package user
