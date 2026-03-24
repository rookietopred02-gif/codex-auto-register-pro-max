# api-register-go (fork)

This fork is focused on the Go-based registration flow and the local dashboard in `api-register-go`.

## Fork-specific changes

- Added `Temp Mail` mode for dashboard-driven registration.
- Switched Temp Mail mode to `tempmail.lol` only.
- Removed the visible "注册转登录" dashboard toggle.
- The Go flow now always uses `create_account -> login -> password -> OTP -> token/workspace` after account creation succeeds.
- Workspace/token acquisition is intentionally deferred until the post-create re-login succeeds, instead of consuming the post-create blocker page directly.
- Added dashboard controls for:
  - Temp Mail parallel on/off
  - Worker count
  - Next-account delay
- Bound the Temp Mail mailbox creation cooldown to the same delay setting used for switching to the next account.
- Added terminal fallback for manual mailbox input and manual OTP input.
- Tightened OTP extraction to only capture `ChatGPT`-related 6-digit codes.
- Improved Temp Mail polling to reduce unnecessary requests and avoid missing late-arriving codes.

## Run

1. Start `register.exe`, or run:

```bash
go run .
```

2. Open:

```text
http://localhost:8899
```

## Notes

- Temp Mail parallel mode can trigger provider rate limits. In practice, `2-5` workers is the safer range.
- Temp Mail mode now uses `tempmail.lol` only. `API Base URL` can be changed for compatible endpoints.
- The intended success path is: create account first, then reopen the login flow and finish password + email OTP before fetching workspace/token.
- This means `add_phone` is treated as a blocker on the direct post-create path, not as the source for workspace extraction.
- Successful tokens are written to the `tokens/` directory.
- Local runtime/config artifacts such as `tokens/`, `*.exe`, and temporary debug files are intentionally not meant for Git tracking.

## Camoufox sidecar scaffold

- `browser_sidecar.go` provides a small JSON-over-stdin/stdout contract for an external browser worker.
- `camoufox_worker.py` now supports `handshake`, a sessionized `start_flow` that can advance to `awaiting_otp`, and `submit_otp` session reuse.
- Browser session data is persisted under `.camoufox-sessions/<session_id>/`, so later steps can reopen the same profile.
- The intent is to let Go keep the existing retry / Temp Mail / token persistence flow, while a short-lived external browser process only owns the high-risk OpenAI browser steps.

## Build

```bash
go build -o register.exe .
```

## Acknowledgements

- Part of the Temp Mail handling and the post-create re-login strategy was cross-checked against [`moeacgx/codex-manager`](https://github.com/moeacgx/codex-manager).
