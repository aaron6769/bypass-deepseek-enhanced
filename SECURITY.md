# Security Policy

## Supported version

Security fixes are applied to the latest commit on the `main` branch and to the latest published container image.

## Reporting a vulnerability

Please use GitHub's private **Report a vulnerability** flow on this repository when it is available. If private reporting is unavailable, contact the maintainer privately before opening a public issue.

Do not include session cookies, access tokens, request headers, production logs, or private prompts in a report. Use synthetic data and redact all credentials. A useful report should include the affected commit or image digest, reproduction steps, expected impact, and any suggested mitigation.

## Container supply chain

The primary container is built from this repository. The browser-enabled Dockerfile additionally consumes an upstream helper archive; its source commit and SHA-256 digest are pinned in `deploy/Dockerfile-BL` and validated during the build.
