/**
 * DARI browser SDK (`dari.web/1`).
 *
 * WebTransport (HTTP/3) primary carrier with a constrained WebSocket
 * fallback. Authorization is a browser-held Ed25519 proof of possession
 * bound to the exact origin and the per-connection channel binding —
 * cookies and bearer tokens are never sufficient.
 */

export type ProfileResult = {
  profile: string;
  status: "EXACT" | "DEGRADED" | "UNSUPPORTED";
  omitted?: string[];
  reason?: string;
};

export interface DariKey {
  publicKeyRaw(): Promise<Uint8Array>;
  sign(message: Uint8Array): Promise<Uint8Array>;
}

type Hello = {
  type: "hello";
  challenge_id?: string;
  nonce?: string;
  binding_token: string;
};

type OpenAck = {
  type: "open_ack";
  session_id?: string;
  resumed?: boolean;
  error?: string;
};

const textDecoder = new TextDecoder();
const textEncoder = new TextEncoder();

function hexEncode(bytes: Uint8Array): string {
  return Array.from(bytes)
    .map((b) => b.toString(16).padStart(2, "0"))
    .join("");
}

function hexDecode(hex: string): Uint8Array {
  const out = new Uint8Array(hex.length / 2);
  for (let i = 0; i < out.length; i++) {
    out[i] = parseInt(hex.slice(i * 2, i * 2 + 2), 16);
  }
  return out;
}

/** SHA-256 of the concatenated inputs. */
async function sha256(...parts: Uint8Array[]): Promise<Uint8Array> {
  const total = parts.reduce((n, p) => n + p.length, 0);
  const buf = new Uint8Array(total);
  let off = 0;
  for (const p of parts) {
    buf.set(p, off);
    off += p.length;
  }
  return new Uint8Array(await crypto.subtle.digest("SHA-256", buf));
}

/** The F.3 channel binding over the carrier token. */
async function channelBinding(token: Uint8Array): Promise<Uint8Array> {
  return sha256(textEncoder.encode("DARI-CHANNEL-BINDING-v1\0"), token);
}

/** Canonical proof signing bytes (mirrors the server). */
async function proofSigningBytes(
  origin: string,
  reconnectSession: string,
  challengeId: string,
  nonce: Uint8Array,
  binding: Uint8Array,
  thumbprint: Uint8Array,
): Promise<Uint8Array> {
  const lp = async (...parts: Uint8Array[]) => {
    const len = new Uint8Array(4);
    new DataView(len.buffer).setUint32(0, parts.length, false);
    return [len, ...parts];
  };
  const domain = textEncoder.encode("DARI-WEB-PROOF-v1\0");
  const o = await lp(textEncoder.encode(origin));
  const r = await lp(textEncoder.encode(reconnectSession));
  const t = await lp(thumbprint);
  const c = await lp(textEncoder.encode(challengeId));
  const n = await lp(nonce);
  const b = await lp(binding);
  return sha256(domain, ...o, ...r, ...t, ...c, ...n, ...b);
}

/** WebCrypto Ed25519 key pair wrapped as a DariKey. */
export class WebCryptoKey implements DariKey {
  keyPair: CryptoKeyPair;

  private constructor(keyPair: CryptoKeyPair) {
    this.keyPair = keyPair;
  }

  static async generate(): Promise<WebCryptoKey> {
    const keyPair = (await crypto.subtle.generateKey(
      { name: "Ed25519" } as Algorithm,
      false,
      ["sign", "verify"],
    )) as CryptoKeyPair;
    return new WebCryptoKey(keyPair);
  }

  async publicKeyRaw(): Promise<Uint8Array> {
    return new Uint8Array(await crypto.subtle.exportKey("raw", this.keyPair.publicKey) as ArrayBuffer);
  }

  async sign(message: Uint8Array): Promise<Uint8Array> {
    return new Uint8Array(await crypto.subtle.sign({ name: "Ed25519" } as Algorithm, this.keyPair.privateKey, message.slice().buffer as ArrayBuffer) );
  }
}

interface Carrier {
  readHello(): Promise<Hello>;
  requestChallenge(origin: string): Promise<{ id: string; nonce: Uint8Array }>;
  sendOpen(open: Record<string, string>): Promise<OpenAck>;
  send(seq: bigint, envelope: Uint8Array): Promise<Uint8Array | null>;
  status(opId: string): Promise<Uint8Array | null>;
  close(): void;
}

/** Length-prefixed frame helpers shared by both carriers. */
function writeFrame(...chunks: Uint8Array[]): Uint8Array {
  const body = chunks.length === 1 ? chunks[0] : concat(chunks);
  const out = new Uint8Array(4 + body.length);
  out[0] = body.length >>> 24;
  out[1] = (body.length >>> 16) & 0xff;
  out[2] = (body.length >>> 8) & 0xff;
  out[3] = body.length & 0xff;
  out.set(body, 4);
  return out;
}

function concat(chunks: Uint8Array[]): Uint8Array {
  const total = chunks.reduce((n, c) => n + c.length, 0);
  const out = new Uint8Array(total);
  let off = 0;
  for (const c of chunks) {
    out.set(c, off);
    off += c.length;
  }
  return out;
}

/** WebSocket fallback carrier. */
class WebSocketCarrier implements Carrier {
  private nextId = 0;
  private pending: { resolve: (v: Uint8Array | null) => void }[] = [];
  private ws: WebSocket;

  private constructor(ws: WebSocket) {
    this.ws = ws;
    ws.binaryType = "arraybuffer";
    ws.addEventListener("message", (ev) => this.onMessage(ev));
  }

  static async dial(url: string, origin: string): Promise<WebSocketCarrier> {
    return new Promise((resolve, reject) => {
      const ws = new WebSocket(url, undefined as never);
      ws.binaryType = "arraybuffer";
      const onOpen = () => {
        ws.removeEventListener("open", onOpen);
        resolve(new WebSocketCarrier(ws));
      };
      const onErr = () => reject(new Error("websocket dial failed"));
      ws.addEventListener("open", onOpen);
      ws.addEventListener("error", onErr);
      void origin;
    });
  }

  private onMessage(ev: MessageEvent) {
    if (typeof ev.data === "string") {
      const msg = JSON.parse(ev.data);
      if (msg.type === "open_ack" || msg.type === "error") return; // handled by awaiting callers
      return;
    }
    const buf = new Uint8Array(ev.data as ArrayBuffer);
    if (buf.length < 8) return;
    const waiter = this.pending.shift();
    if (waiter) waiter.resolve(buf.subarray(8));
  }

  async readHello(): Promise<Hello> {
    return this.readControl<Hello>((msg) => msg.type === "hello");
  }

  private readControl<T>(match: (msg: any) => boolean): Promise<T> {
    return new Promise((resolve) => {
      const handler = (ev: MessageEvent) => {
        if (typeof ev.data !== "string") return;
        const msg = JSON.parse(ev.data);
        if (!match(msg)) return;
        this.ws.removeEventListener("message", handler);
        resolve(msg);
      };
      this.ws.addEventListener("message", handler);
    });
  }

  async requestChallenge(origin: string): Promise<{ id: string; nonce: Uint8Array }> {
    this.ws.send(JSON.stringify({ type: "challenge_request", origin }));
    const reply = await this.readControl<any>((m) => m.type === "challenge" || m.type === "error");
    if (reply.error) throw new Error(reply.error);
    return { id: reply.challenge_id, nonce: hexDecode(reply.nonce) };
  }

  async sendOpen(open: Record<string, string>): Promise<OpenAck> {
    this.ws.send(JSON.stringify(open));
    return this.readControl<OpenAck>((m) => m.type === "open_ack" || m.type === "error");
  }

  async send(seq: bigint, envelope: Uint8Array): Promise<Uint8Array | null> {
    const seqBuf = new Uint8Array(8);
    new DataView(seqBuf.buffer).setBigUint64(0, seq, false);
    this.ws.send(concat([seqBuf, envelope]));
    void this.nextId;
    return new Promise((resolve) => {
      this.pending.push({ resolve });
    });
  }

  async status(_opId: string): Promise<Uint8Array | null> {
    return null; // Status flows over control; callers use status().
  }

  close(): void {
    this.ws.close();
  }
}

/** WebTransport primary carrier (HTTP/3). */
class WebTransportCarrier implements Carrier {
  private session: WebTransport;
  private stream: WebTransportBidirectionalStream;

  private constructor(session: WebTransport, stream: WebTransportBidirectionalStream) {
    this.session = session;
    this.stream = stream;
  }

  static async dial(url: string): Promise<WebTransportCarrier> {
    const session = new WebTransport(url);
    await session.ready;
    const stream = await session.createBidirectionalStream();
    // Ready byte: client WT streams become visible to the server only
    // after the first write.
    const w = stream.writable.getWriter();
    await w.write(new Uint8Array([0x00]));
    w.releaseLock();
    return new WebTransportCarrier(session, stream);
  }

  private async readFrame(): Promise<Uint8Array> {
    const r = this.stream.readable.getReader();
    try {
      const lenHead = await this.readFull(r, 4);
      const len = new DataView(lenHead.buffer).getUint32(0, false);
      return await this.readFull(r, len);
    } finally {
      r.releaseLock();
    }
  }

  private async readFull(reader: ReadableStreamDefaultReader<Uint8Array>, n: number): Promise<Uint8Array> {
    const out = new Uint8Array(n);
    let off = 0;
    while (off < n) {
      const { value, done } = await reader.read();
      if (done) throw new Error("stream closed");
      out.set(value.subarray(0, n - off), off);
      off += value.length;
    }
    return out;
  }

  private async writeControl(obj: unknown): Promise<void> {
    const w = this.stream.writable.getWriter();
    try {
      const payload = textEncoder.encode(JSON.stringify(obj));
      const frame = new Uint8Array(1 + payload.length);
      frame[0] = 0x01;
      frame.set(payload, 1);
      await w.write(writeFrame(frame));
    } finally {
      w.releaseLock();
    }
  }

  async readHello(): Promise<Hello> {
    const frame = await this.readFrame();
    return JSON.parse(textDecoder.decode(frame.subarray(1)));
  }

  async requestChallenge(origin: string): Promise<{ id: string; nonce: Uint8Array }> {
    await this.writeControl({ type: "challenge_request", origin });
    const frame = await this.readFrame();
    const reply = JSON.parse(textDecoder.decode(frame.subarray(1)));
    if (reply.error) throw new Error(reply.error);
    return { id: reply.challenge_id, nonce: hexDecode(reply.nonce) };
  }

  async sendOpen(open: Record<string, string>): Promise<OpenAck> {
    await this.writeControl(open);
    const frame = await this.readFrame();
    return JSON.parse(textDecoder.decode(frame.subarray(1)));
  }

  async send(seq: bigint, envelope: Uint8Array): Promise<Uint8Array | null> {
    const w = this.stream.writable.getWriter();
    try {
      const seqBuf = new Uint8Array(8);
      new DataView(seqBuf.buffer).setBigUint64(0, seq, false);
      const body = new Uint8Array(1 + 8 + envelope.length);
      body[0] = 0x02;
      body.set(seqBuf, 1);
      body.set(envelope, 9);
      await w.write(writeFrame(body));
    } finally {
      w.releaseLock();
    }
    const frame = await this.readFrame();
    if (frame.length < 9) return null;
    return frame.subarray(9);
  }

  async status(opId: string): Promise<Uint8Array | null> {
    await this.writeControl({ type: "status_query", operation_id: opId });
    const frame = await this.readFrame();
    const reply = JSON.parse(textDecoder.decode(frame.subarray(1)));
    if (reply.error) throw new Error(reply.error);
    return hexDecode(reply.envelope ?? "");
  }

  close(): void {
    void this.session.close();
  }
}

export interface OpenOptions {
  relay: string; // e.g. https://relay.example
  origin: string; // window.location.origin
  key: DariKey;
  preferWebSocketFallback?: boolean;
  reconnectSessionId?: string;
}

export class DariClient {
  profiles: ProfileResult[] = [];
  private seq = 1n;
  private carrier: Carrier;
  sessionId: string;
  resumed: boolean;

  private constructor(carrier: Carrier, sessionId: string, resumed: boolean) {
    this.carrier = carrier;
    this.sessionId = sessionId;
    this.resumed = resumed;
  }

  /** Generate a fresh browser key (WebCrypto Ed25519). */
  static async generateKey(): Promise<DariKey> {
    return WebCryptoKey.generate();
  }

  static async open(opts: OpenOptions): Promise<DariClient> {
    const carrier = opts.preferWebSocketFallback
      ? await WebSocketCarrier.dial(opts.relay.replace(/^http/, "ws") + "/dari.web/1", opts.origin)
      : await WebTransportCarrier.dial(opts.relay + "/dari.web/1");

    const hello = await carrier.readHello();
    const binding = await channelBinding(hexDecode(hello.binding_token));
    const challenge = await carrier.requestChallenge(opts.origin);

    const pub = await opts.key.publicKeyRaw();
    const thumbprint = await sha256(textEncoder.encode("DARI-SUBJECT-KEY-v1\0"), pub);
    const signing = await proofSigningBytes(
      opts.origin,
      opts.reconnectSessionId ?? "",
      challenge.id,
      challenge.nonce,
      binding,
      thumbprint,
    );
    const signature = await opts.key.sign(signing);

    const ack = await carrier.sendOpen({
      type: "open",
      origin: opts.origin,
      challenge_id: challenge.id,
      reconnect_session: opts.reconnectSessionId ?? "",
      subject_key: hexEncode(pub),
      signature: hexEncode(signature),
    });
    if (ack.error) throw new Error(`DARI open rejected: ${ack.error}`);
    return new DariClient(carrier, ack.session_id ?? "", ack.resumed ?? false);
  }

  /** Send one canonical DARI envelope; returns the governed response. */
  async send(envelope: Uint8Array): Promise<Uint8Array | null> {
    const seq = this.seq++;
    return this.carrier.send(seq, envelope);
  }

  /** Query an operation's durable status (reconnect-safe; never re-executes). */
  async status(operationId: string): Promise<Uint8Array | null> {
    return this.carrier.status(operationId);
  }

  /** Close the carrier; server-side session state persists for reconnect. */
  close(): void {
    this.carrier.close();
  }

  /** Resume a prior session on a fresh carrier (fresh proof required). */
  static async reconnect(sessionId: string, opts: OpenOptions): Promise<DariClient> {
    return DariClient.open({ ...opts, reconnectSessionId: sessionId });
  }
}


// Exported pure helpers for the SDK test vectors.
export function hexEncodeVector(bytes: Uint8Array): string {
  return hexEncode(bytes);
}
export function hexDecodeVector(hex: string): Uint8Array {
  return hexDecode(hex);
}
