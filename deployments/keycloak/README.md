# Patty Keycloak deployment contract

PAT-1564 keeps Patty on Keycloak and moves public identities from the `sso`
realm to `patty`. The canonical public issuer is
`https://login.patty.io/realms/patty`. Public and workforce users share one
Keycloak instance but never a realm, client, user store, signing key, policy,
or self-registration surface.

## Pinned supply chain

- Keycloak: `26.7.1`, pinned to OCI index digest
  `sha256:f1f1f01e472c8a78df40d8f2a49a925274eda4d3d80d5f6edbb5c880ee3c01c6`.
- Apple provider: `1.17.0`, Apache-2.0, pinned to SHA-256
  `4091dee2a1ec9e0771bef4bd46005197d86b0a2b1f25198c41738476b1d102bb`.
- Naver provider: owned source in `providers/naver`, compiled against Keycloak
  `26.7.1`. Its immutable Naver `response.id` is the subject; email remains an
  optional, untrusted attribute.

The latest Keycloak release was checked on 2026-08-22. Upgrade by changing the
version and digest together, compiling both SPIs, running the import smoke test,
and completing the rollback rehearsal below. Never deploy a floating tag.

## Local import proof

Run the Go contract and provider tests first:

```sh
go test ./internal/keycloakconfig
(cd deployments/keycloak/providers/naver && mvn --batch-mode --no-transfer-progress package)
```

With Docker available:

```sh
docker compose -f deployments/keycloak/docker-compose.validation.yml up --build --wait
curl --fail http://127.0.0.1:18080/realms/patty/.well-known/openid-configuration
docker compose -f deployments/keycloak/docker-compose.validation.yml down --volumes
```

The validation compose file uses `start-dev` only for the disposable import
proof. Production uses `docker-compose.production.yml`, PostgreSQL, TLS at the
reverse proxy, and `start --optimized`.

## Authority split

Keycloak is the only interactive authentication authority. It owns passwords,
verification and recovery challenges, MFA/passkeys, upstream social/federated
authentication, and browser/device sessions. The `id.patty.io` account worker
owns Patty profile/domain data, subscription state, device records, and the
compatibility adapter that resolves `(issuer, subject)` to a Patty Account.
It must not mint an independent password/session after cutover.

Authentication does not authorize inference. The chain is:

`Keycloak login → Patty Account identity resolution → subscription/customer policy → Harness enrollment → DARI peer credential → Working Session + Capability Lease`.

A user without an active entitlement may manage their account and devices, but
inference returns an entitlement/upgrade response, never an authentication
error. Renewal restores entitlement without replacing the Harness identity;
revoking one Harness does not sign other devices out.

## Realm inventory and isolation gate

Use a short-lived admin account and a temporary `kcadm` config outside the
repository. Then run `inventory.sh OUTPUT.json`. The output intentionally omits
provider/client secrets and is mode `0600`. Review every realm for clients,
exact redirect URIs, scopes, IdPs, flows, password/MFA/brute-force policy,
themes, token lifetimes, and user count.

The production gate fails if:

- a public client or public user exists in the workforce realm;
- the `patty` realm contains a workforce client, shared signing key, or
  cross-realm mapper;
- any product-facing realm/client label exposes `sso` as a name;
- wildcard web origins exist, or browser clients enable implicit/password
  grants;
- signing-key rotation, recovery mail, event/audit retention, or backup restore
  has no current evidence.

Do not commit an inventory containing user records, tokens, credentials, keys,
or provider configuration secrets.

## Social providers

All four providers are declared disabled in the import. Enable one only after
its credentials are installed through Keycloak's secret-management path and a
test account proves the returned immutable subject and consented claims.

- Google: require `openid profile email`; `trustEmail` remains false.
- Apple: install the pinned SPI; generate its signed client secret outside the
  realm JSON and validate both first-login POST callback and repeat login.
- Kakao: generic OIDC with signature validation and the explicit JWKS URL;
  confirm `account_email` consent in the Kakao application.
- Naver: build/install the owned SPI; confirm the app's `name`, `email`, and
  `profile_image` consent. Missing email still authenticates by Naver subject
  and enters the explicit account-linking flow.

No provider may auto-link by email. PAT-1565's explicit linking and SCIM/JIT
collision rules remain authoritative.

## `sso` → `patty` migration and rollback

1. Export and back up the Keycloak database, themes, and provider artifacts.
   Prove restore into an isolated instance running the old pinned version.
2. Generate the non-secret realm inventory. Import every `sso` user into the
   target realm while preserving Keycloak IDs where the import supports it.
   Federated users keep their upstream provider; no password/token is copied by
   the compatibility bridge.
3. Import an `internal/ssomigrate` manifest whose `source` is the exact old
   issuer. Every non-user item needs `keep`, `compat_bridge`, or `retire`; every
   user needs one exact issuer+subject link or an explicit retirement.
4. Reconcile until `source_count == target_count`, ambiguity/conflict are zero,
   and status is `wave_ready`. The service refuses sign-off before parity or
   without a rollback window.
5. Cut over in waves: low-risk app, Patty Account/Web, PCCP console, then
   CLI/Desktop. Each owner records login/logout/recovery, mobile system-browser
   PKCE, RFC 8628 device verification/code/expiry/polling/resume, monitoring,
   and rollback evidence.
6. The issuer change intentionally invalidates old sessions. Communicate the
   one-time reauthentication. Roll back by restoring the old application issuer
   and database snapshot within the signed window; never copy sessions.
7. Disable `sso` only after all waves pass. Retire it only after the agreed
   zero-traffic window and a final export. Keep the export under the retention
   policy, not in Git or Linear.

## Enterprise and sovereign boundary

Enterprise console identities arrive through each customer's OIDC/SAML IdP and
SCIM lifecycle in `internal/sso`; their entitlement comes from customer policy,
never the consumer subscription module. Deprovisioning must revoke console
sessions and Harness credentials. Sovereign deployments use the same realm
contract locally with internal CAs, offline update artifacts, external checks
disabled, and customer-operated backups; Kerberos/SPNEGO and X.509 flows are
enabled only in that local trust domain.
