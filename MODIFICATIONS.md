# Modification history

This repository is based on [`xllm-go/bypass`](https://github.com/xllm-go/bypass).

## 2026-07-15 — DeepSeek Web enhancement

Base revision: `2c0c631` (`xllm-go/bypass`, `main`).

Changes maintained in this repository:

- Updated the DeepSeek Web request profile and completion payload handling.
- Added explicit fast, expert, search, and vision mode mappings.
- Added thinking and non-thinking model aliases while retaining the legacy `deepseek-chat` and `deepseek-reasoner` aliases.
- Added image loading, upload, status polling, and vision prompt support.
- Added a local DeepSeek PoW solver and deterministic test vectors.
- Updated JSON Patch/SSE parsing to preserve fragment state across pathless patches.
- Routed `THINK` fragments to `reasoning_content` and `RESPONSE` fragments to `content` for both streaming and non-streaming responses.
- Preserved the first final-answer character when DeepSeek appends a new response fragment.
- Redacted authorization, API key, and cookie headers from debug request dumps.
- Improved Windows `toolexec` builds by ensuring the host Go binary remains discoverable.
- Added regression coverage for model mapping, PoW, image payloads, request redaction, and response parsing.

Local deployment scripts, credentials, runtime configuration, and environment-specific live-test helpers are intentionally excluded from the public repository.

The upstream copyright and GPL-3.0 license remain in effect. Changes in this repository are distributed under the same license.
