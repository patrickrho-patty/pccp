#!/usr/bin/env bash
set -euo pipefail

# Produces a non-secret realm inventory from an already authenticated kcadm
# session. Authenticate with a temporary --config file before invoking this
# script; never pass an admin password to this process or commit its output.
kcadm_bin=${KCADM_BIN:-kcadm.sh}
kcadm_config=${KCADM_CONFIG:?set KCADM_CONFIG to an authenticated temporary kcadm config}
output_path=${1:?usage: inventory.sh OUTPUT.json}

command -v jq >/dev/null
command -v "$kcadm_bin" >/dev/null

tmp_dir=$(mktemp -d)
trap 'rm -rf "$tmp_dir"' EXIT

"$kcadm_bin" get realms --config "$kcadm_config" > "$tmp_dir/realms.json"
fetch_realm() {
	local index=$1
	local realm=$2
	local realm_dir="$tmp_dir/realm-$index"
	local pids=()
  mkdir -p "$realm_dir"
  "$kcadm_bin" get "clients?max=10000" -r "$realm" --config "$kcadm_config" > "$realm_dir/clients.json" &
	pids+=("$!")
  "$kcadm_bin" get client-scopes -r "$realm" --config "$kcadm_config" > "$realm_dir/scopes.json" &
	pids+=("$!")
  "$kcadm_bin" get identity-provider/instances -r "$realm" --config "$kcadm_config" > "$realm_dir/idps.json" &
	pids+=("$!")
  "$kcadm_bin" get authentication/flows -r "$realm" --config "$kcadm_config" > "$realm_dir/flows.json" &
	pids+=("$!")
  "$kcadm_bin" get "users/count" -r "$realm" --config "$kcadm_config" > "$realm_dir/user-count.json" &
	pids+=("$!")
	for pid in "${pids[@]}"; do
		wait "$pid"
	done
  jq -n --arg realm "$realm" \
    --slurpfile clients "$realm_dir/clients.json" \
    --slurpfile scopes "$realm_dir/scopes.json" \
    --slurpfile idps "$realm_dir/idps.json" \
    --slurpfile flows "$realm_dir/flows.json" \
    --slurpfile user_count "$realm_dir/user-count.json" '{
      realm: $realm,
      user_count: $user_count[0],
      clients: ($clients[0] | map({clientId, enabled, publicClient, standardFlowEnabled, implicitFlowEnabled, directAccessGrantsEnabled, redirectUris, webOrigins, defaultClientScopes, optionalClientScopes})),
      client_scopes: ($scopes[0] | map({name, protocol, attributes})),
      identity_providers: ($idps[0] | map({alias, displayName, providerId, enabled, trustEmail, storeToken, linkOnly})),
      authentication_flows: ($flows[0] | map({alias, providerId, topLevel, builtIn}))
    }' > "$realm_dir/details.json"
}

batch_count=0
realm_pids=()
while IFS=$'\t' read -r index realm; do
  fetch_realm "$index" "$realm" &
	realm_pids+=("$!")
  batch_count=$((batch_count + 1))
  if (( batch_count % 4 == 0 )); then
		for pid in "${realm_pids[@]}"; do
			wait "$pid"
		done
		realm_pids=()
  fi
done < <(jq -r 'to_entries[] | [.key, .value.realm] | @tsv' "$tmp_dir/realms.json")
for pid in "${realm_pids[@]}"; do
	wait "$pid"
done

jq -s --slurpfile realms "$tmp_dir/realms.json" '
  . as $details |
  {generated_at: now | todate, realms: ($realms[0] | map({
    realm, displayName, enabled, registrationAllowed, verifyEmail,
    duplicateEmailsAllowed, bruteForceProtected, passwordPolicy,
    accessTokenLifespan, ssoSessionIdleTimeout, ssoSessionMaxLifespan,
    loginTheme, accountTheme, emailTheme, supportedLocales, defaultLocale
  }) | map(. as $realm | . + (($details[] | select(.realm == $realm.realm)) // {})))}
' "$tmp_dir"/realm-*/details.json > "$tmp_dir/inventory.json"

install -m 0600 "$tmp_dir/inventory.json" "$output_path"
printf 'wrote non-secret realm inventory to %s\n' "$output_path"
