/**
 * SDK test — pure-helper vectors (node --experimental-strip-types or
 * any TS test runner). Runtime carrier vectors live in the Go
 * conformance suite (internal/webbinding), which drives the REAL
 * WebTransport and WebSocket carriers end-to-end.
 */
import { hexEncodeVector, hexDecodeVector } from "./client.ts";

console.assert(
  hexEncodeVector(new Uint8Array([0, 1, 2, 0xfe, 0xff])) === "000102feff",
  "hex encode vector",
);
const decoded = hexDecodeVector("000102feff");
console.assert(
  decoded.length === 5 && decoded[3] === 0xfe && decoded[4] === 0xff,
  "hex decode vector",
);
console.log("sdk web vectors ok");
