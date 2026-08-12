# Contributing

## Development workflow

1. Create a focused branch from `main`.
2. Keep credentials, cookies, private prompts, and production configuration out of commits and test output.
3. Add or update regression tests for behavior changes.
4. Run the relevant Go tests before opening a pull request:

   ```bash
   go test ./core/common/toolcall ./core/gin ./relay/llm/deepseek
   ```

5. For container changes, verify both `deploy/Dockerfile` and `deploy/Dockerfile-BL`. GitHub Actions performs the authoritative Buildx builds.

Pull requests should explain the root cause, user impact, validation performed, and any operational migration required.
