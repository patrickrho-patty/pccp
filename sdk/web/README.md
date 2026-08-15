# DARI Web SDK (`dari.web/1`)

Browser client for the DARI governed protocol.

- **Carrier**: WebTransport over HTTP/3 (primary) with a constrained
  WebSocket fallback carrying the identical canonical DARI envelope.
- **Authorization**: a browser-held Ed25519 key (WebCrypto, non-extractable
  where supported) proves possession bound to the exact origin and the
  per-connection channel binding. Cookies/bearer tokens alone never
  authenticate — the server rejects them without a proof.
- **Reconnect**: sessions resume server-side state (last sequence, grant
  digest, effect operation IDs); after an uncertain disconnect the SDK
  queries `EFFECT_STATUS` instead of re-submitting an operation.
- **No bypass**: the SDK never contacts model endpoints directly; every
  envelope flows through the governed relay path.

## Usage

```ts
import { DariClient } from "./src/client";

const key = await DariClient.generateKey();
const client = await DariClient.open({
  relay: "https://relay.example",
  origin: window.location.origin,
  key,
});
const result = await client.send(envelopeBytes);
const status = await client.status("op-42"); // reconnect-safe
```

## Profile results

The runtime reports `EXACT | DEGRADED | UNSUPPORTED` per profile offer;
this SDK surfaces the relay's negotiation results verbatim.
