# Enterprise customer lifecycle runbook

This runbook is the release gate for an enterprise or sovereign customer. A
customer is not production-ready until every evidence item below has an owner,
timestamp, and rollback decision in the customer change record.

## Pilot sequence and tier

1. Create the organization with the final deployment profile and data region.
2. Configure customer OIDC or SAML and a tenant-bound SCIM token. Verify issuer,
   immutable subject, group mapping, and IdP signing-key rotation in staging.
3. Import users and groups through SCIM. Do not use email-domain inference as
   identity authority.
4. Set `max_user_seats` and `max_harness_seats` to the contracted pilot cohort.
   Start with the contracted named user cohort, 1–3 repositories, one model
   route, and one network zone. Increasing either limit is an explicit pilot
   change.
5. Install the enrollment policy with `PUT /api/harnesses/enrollment-policy`.
   Enterprise pilots should require an administrator code, MDM, named posture
   controls, attestation, approved network zones, and Ed25519 release keys.
   Sovereign policy cannot be saved without release-signing trust.
6. Generate a user-bound one-time enrollment code, enroll the harness, and
   retain the `cp.harness.enrolled` and
   `cp.harness.enrollment_policy_updated` audit events.
7. Exercise one allowed and one denied model request, repository boundary,
   policy exception, user suspension, and SCIM deprovision before expanding.

An active or suspended user consumes a user seat; only terminally offboarded
users release it. Pending, enrolled, active, and quarantined harnesses consume a
harness seat; only revoked harnesses release it. The organization row is locked
while counting and issuing a harness, so concurrent enrollments cannot exceed
the configured limit.

## Enrollment evidence

The enrollment request carries `mdm_enrolled`, the JSON `mdm_posture` proof,
`attestation`, `attested_at`, `network_zone`, `ip_address`, `binary_hash`, and
`build_signature`. The release pipeline signs these bytes with Ed25519:

```text
PCCP-HARNESS-BUILD-v1\0<organization_id>\0<binary_hash>
```

The MDM or attestation authority separately signs the fresh canonical device
evidence prefixed by `PCCP-DEVICE-ATTESTATION-v1`. It binds organization, user,
harness ID and public key, build hash, MDM posture, network zone, and
`attested_at`. PCCP accepts timestamps no more than five minutes old (with one
minute of forward clock skew) and only keys installed in the organization
policy.

PCCP evaluates every configured gate before creating a device, issuing a PPC,
or creating a harness. A failed request returns 403 and the transaction also
rolls back any attempted one-time-code consumption. Persisted device and
harness records retain the accepted posture, network zone, and attestation
time for audit.

## Customer IdP outage policy

The control plane does not convert an IdP outage into weaker authentication.

| Path | During outage | Recovery gate |
| --- | --- | --- |
| New OIDC/SAML login | Denied; no cached-password fallback | Verified IdP callback and current tenant configuration |
| Existing console token | Valid only until its signed 24-hour expiry and only while the organization, user lifecycle epoch, and current grants still match | Normal reauthentication |
| SCIM provisioning | Failed request; retry idempotently from the customer IdP | Reconcile users/groups and inspect remaining-access evidence |
| Existing harness PPC/lease | Valid only to its signed expiry and current revocation/lifecycle state | Normal lease/PPC renewal |
| Sovereign offline site | Valid only under an unexpired, deployment-bound, monotonically increasing signed entitlement | Import a newer signed artifact |

Do not extend token expiry, mint local emergency users, or disable lifecycle
introspection during an outage. A break-glass console administrator must be a
pre-created, hardware-protected, audited customer decision and is rehearsed on
the customer-approved schedule; it is not created during the incident.

## Proxy and TLS acceptance matrix

Run every applicable row from the customer network. Record DNS result, proxy
decision, certificate chain, SNI, ALPN, HTTP status, and timestamp.

| Route | Direct | Explicit proxy | TLS inspection | Expected result |
| --- | --- | --- | --- | --- |
| Browser → PCCP console/API | Required | Customer-dependent | Customer CA trusted only when approved | TLS 1.2+; correct hostname; no mixed content |
| PCCP → OIDC discovery/JWKS | Customer-dependent | Required when configured | Validate customer-approved CA chain | Discovery and key rotation succeed; wrong issuer fails |
| Customer IdP → SCIM | Required from IdP egress | Customer-dependent | Validate customer-approved CA chain | Tenant-bound token succeeds; other tenant/token fails |
| Harness → relay/control plane | Required or private route | Customer-dependent | Do not terminate DARI peer authentication | PPC and transcript binding remain valid |
| PCCP/relay → model endpoint | According to route policy | Customer-dependent | Preserve endpoint identity/attestation | Allowed model works; unlisted endpoint fails |
| Sovereign deployment → public Internet | Forbidden | Forbidden | Not applicable | Dial denied; signed offline channel remains usable |

The native DARI relay never uses an automatically generated certificate in
production. Mount the relay certificate and key from the deployment secret
store and set `PCCP_RELAY_TLS_CERT` and `PCCP_RELAY_TLS_KEY`; PCCP refuses to
start without both unless `PCCP_DEV_BOOTSTRAP=1` is explicitly enabled. A
harness uses the operating-system roots for a public certificate. For a
customer/private CA, distribute only the CA chain and configure
`DARI_RELAY_CA_FILE`; use `DARI_RELAY_TLS_SERVER_NAME` when the network address
differs from the certificate's DNS name. `DARI_DEVELOPMENT_INSECURE_TLS=1` is
development-only and is rejected by sovereign mode.

Also test expired certificates, missing intermediates, wrong SNI, proxy 407,
JWKS rotation, DNS failure, connection timeout, and a proxy that strips upgrade
headers. A success-only matrix is not sufficient.

## Offboarding and contract end

SCIM DELETE or `active=false` uses the canonical terminal lifecycle transition.
It revokes console sessions, capability leases, harness/device bindings,
unused enrollment codes, role entitlements, project memberships, and console
credentials in one transaction. The response includes `remaining_access` and
cleanup failures; production sign-off requires `remaining_access == 0` and no
unresolved cleanup failure.

The scheduled contractor sweep performs the same terminal transition after
`contract_end`; suspension is not considered contract-end offboarding. For a
bulk event, first confirm another active organization administrator remains,
then send tenant-scoped SCIM deprovision requests in bounded batches. Retry
idempotently, export the `cp.user.offboarded`/reconciliation audit events, and
query for any nonterminal session, lease, harness, device, enrollment,
entitlement, project membership, or console credential before closing the
change.

## Sovereign offline entitlement

The offline issuer signs the canonical `OfflineEntitlement` JSON prefixed by
`PCCP-OFFLINE-ENTITLEMENT-v1\0`. The artifact binds organization, deployment,
profile, sequence, validity window, seat limits, features, and model classes.
Import it with `POST /api/sovereign/entitlements/{deploymentID}`. Both PCCP and
the harness reject wrong scope, invalid signature, not-yet-valid or expired
time, and a sequence that does not advance. Private signing keys never enter
PCCP or the harness. Set the immutable local installation identity with
`PCCP_SOVEREIGN_DEPLOYMENT_ID`. Install the offline authority during the
deployment ceremony with `PCCP_SOVEREIGN_ENTITLEMENT_AUTHORITIES`, a JSON
object mapping organization IDs to Ed25519 public keys; startup fails if the
durable trust bundle is absent or a configured key would rotate an existing
pin, and the first installation is audited. Configure the harness `[sovereign]`
section with the same organization/deployment IDs, a named trust source and
authority public key, and the operator-delivered `entitlement_file`. The
harness re-verifies that pinned source and artifact at every inference dispatch.

## Production sign-off

- Pilot acceptance and rollback owners recorded.
- SSO login, key rotation, SCIM create/update/delete, and IdP outage tested.
- Enrollment allow and every individual denial gate tested.
- Seat boundaries tested with concurrent enrollment attempts.
- Proxy/TLS negative matrix completed from customer networks.
- Bulk and contractor-expiry offboarding proves zero remaining access.
- Sovereign sites prove online dial denial and signed entitlement expiry,
  tamper, scope, and replay rejection.
- Audit export retained under the contracted evidence policy.
