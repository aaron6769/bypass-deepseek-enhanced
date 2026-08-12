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

## 2026-07-16 — Security, lifecycle, and container hardening

- Corrected DeepSeek tool-call credential/proxy routing with a typed connection value.
- Added bounded image reads, public-address enforcement, DNS pinning, redirect revalidation, timeouts, and image media validation.
- Reassembled long SSE lines and preserved response fragments across scanner buffer boundaries.
- Moved session cleanup ahead of fallible requests and closed response bodies on all relevant paths.
- Removed an inherited hard-coded You.com session cookie; background checks now use configured `you.cookies` only.
- Upgraded the project and container build to Go 1.26.5 and refreshed vulnerable networking and cryptography dependencies.
- Changed both Dockerfiles to build the checked-out enhanced source and added multi-platform GitHub Actions verification and publishing.

## 2026-07-16 — DeepSeek live runtime compatibility

- Changed DeepSeek PoW headers from unpadded raw Base64 to browser-compatible padded Base64, fixing intermittent `MISSING_HEADER` responses and vision uploads.
- Enabled tool selection by default when a standard OpenAI Chat Completions request contains `tools`, while preserving `tool_choice: "none"` and explicit internal disable behavior.
- Updated the DeepSeek tool-response reader for the current `event:` plus JSON Patch/SSE format, ignored thinking fragments during tool selection, and treated a normal stream EOF as success.
- Added regression tests for PoW header encoding, standard tools activation, current tool SSE parsing, and THINK/RESPONSE separation.

## 2026-08-12 — Browser container supply-chain hardening

- Replaced the floating `ubuntu:latest` runtime with Ubuntu 24.04 and modernized the Google Chrome signing-key configuration.
- Pinned the upstream browser helper archive to commit `f27457314bd6e1f135c7f08988ef9521eda4905e` and verify its SHA-256 digest before extraction.
- Extract only the required Linux amd64 helper, remove package caches, and run the browser-enabled image as an unprivileged user.
- Added a real linux/amd64 Buildx build for `Dockerfile-BL`, isolated build-cache scopes, and bounded Actions job runtimes.
- Added Dependabot coverage, contribution guidance, and a security policy.

Local deployment scripts, credentials, runtime configuration, and environment-specific live-test helpers are intentionally excluded from the public repository.

The upstream copyright and GPL-3.0 license remain in effect. Changes in this repository are distributed under the same license.
