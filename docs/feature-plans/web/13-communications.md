# 13 — 커뮤니케이션 허브 · Communications (`web/src/pages/Communications.tsx`)

> Vertical read: component → `fetch /api/communications/{conversations,presence,broadcasts,file-transfers,...}` → comms handlers (1340–1430, 2106–2150) → `communications` service (CreateConversation/SendMessage/etc.) → `models/communications.go`.

## What this page actually is
The **Engineering Communications Hub** (§21–23) — conversations/chat, presence, broadcasts (emergency/admin), and managed file transfer. Designed to be the in-harness coordination surface; today it's a CP-side admin console.

## Current vertical (what exists) — real DB-backed, rich models
| Layer | Reality |
|---|---|
| Component | 4 tabs (chat/broadcast/files/presence); create conversation, send message, send broadcast (severity/target/ack), create file transfer; **polls every 5s** for new messages/broadcasts |
| Handlers/service | all persist (`CreateConversation`, `SendMessage`, `SendBroadcast`, `CreateFileTransfer`, presence) |
| `Message` model | **rich** — `ParentMessageID` (threading), `MentionsJSON`, `ReactionsJSON`, `ReadByJSON`, `ContentEncrypted`+`EncryptionKeyRef`, `LinkedSessionID`/`LinkedExchangeID` (§21.6 AI-context link), `RequiresContextExchange` |
| `FileTransfer` model | **rich** — `StoragePath`, `FileHash`, `ScanStatus`{pending,clean,blocked,failed}, `ScanFindingsJSON`, `Classification`, `EncryptionKeyRef`, `Status` lifecycle, `ExpiresAt` |
| `Broadcast` model | severity, `requires_ack` |

## Gaps — grounded

### A. Real-time & delivery (the core miss)
**A1. Polling, not real-time.** 5s `setInterval` reload — no WebSocket/SSE. *Fix:* SSE/WS fan-out for instant delivery.
**A2. Not delivered to the harness.** Comms is CP-side only; the developer's harness never receives chat/presence/broadcast over DARI (§21.2). *Fix:* a DARI comms channel to enrolled harnesses; admin commands too (§22.4).
**A3. File transfer is metadata-only.** `CreateFileTransfer` records a row; **no upload/download/storage** (`StoragePath` empty), **no scan** (`ScanStatus` stuck `pending`), **no expiry enforcement**. *Fix:* real object storage + DLP/malware scan → `clean`/`blocked` + download flow + expiry job.

### B. Modeled-but-unwired (rich fields the UI ignores)
**B1. Threading** (`ParentMessageID`) — no reply threads in UI.
**B2. Mentions/reactions/read-receipts** — fields exist, not rendered.
**B3. Encryption** — `ContentEncrypted`/`EncryptionKeyRef` present but content stored plaintext (§21.5 E2E missing).
**B4. AI-context linking** (§21.6) — `LinkedSessionID`/`LinkedExchangeID` not settable from UI.
**B5. Broadcast ack** — `requires_ack` set, `AckBroadcast` exists in service, but no ack dashboard/reminder.

### C. Genuinely missing
**C1. Launch 1:1 chat from a user** *(your example)* — comms is disconnected from user search.
**C2. Privacy-aware separation** (§6.5) — a platform admin can read comms content; no role gating.
**C3. Admin command vs message** (§22.4); **C4. Secure handoff** (§23.4); **C5. Retention controls** (§40).

## UX improvements (grounded)
1. 5s polling flicker (A1) → real-time.
2. No 1:1 from user search (C1).
3. No threading/reactions/mentions/read-receipts (B1/B2).
4. No unread indicators / notifications.
5. File transfer no progress/preview/scan-result (A3).
6. Broadcast no recipient preview or ack dashboard (B5).
7. No markdown/rich text; no code blocks.
8. No keyboard shortcuts; no favorites/pinned conversations.
9. No sub-menu (channels/DMs/broadcasts/transfers).
10. No search across messages.
11. No empty-state; no responsive mobile.
12. Presence dots static (no "last active").
13. Severity/target hardcoded selects.
14. No message edit/delete UI (`Edited`/`DeletedBy` fields exist).

## Sequencing
Phase 1 (usability): C1 (1:1 from user), B1/B2 (threading/reactions/mentions), real-time SSE (A1).
Phase 2 (real comms): A2 (deliver to harness over DARI), A3 (real file transfer+scan), B3 (E2E), B5 (ack dashboard).
Phase 3 (enterprise): C2 (privacy gating), C3/C4/C5 (admin commands, handoff, retention).
