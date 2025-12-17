# Plan: Add end-to-end testing with go-rod

## Goals
- Exercise the note-taking UI in a real browser to verify text entry, persistence, and live update flows.
- Keep tests hermetic by starting/stopping a local jotter server within the test harness.
- Make running the suite easy for contributors and CI via `go test`/`task` commands.

## Prerequisites
- go-rod and its launcher helpers added to `go.mod` (e.g., `github.com/go-rod/rod` and `github.com/go-rod/rod/lib/launcher`).
- Headless-capable Chromium/Chrome available; default to downloading via Rod if `rod` cache is empty, but allow overriding with `ROD_CHROME_PATH`.
- Keep everything inside existing packages; add rod as a test-only dependency and place tests alongside the code in the relevant `_test.go` files.

## Test harness design
1. **Server bootstrap**
   - Reuse the existing server constructor (`NewServer`) and start it on a random free port (use `net.Listen` to reserve, then close and pass the port).
   - Point `JOT_DIR` to a temp directory per test to isolate jot files.
   - Run the server in a goroutine and ensure graceful shutdown using a `context` cancel function in test cleanup.

2. **Rod session setup**
   - Create a helper to launch a headless browser with sensible defaults (`launcher.New().Headless(true)`), honoring `ROD_CHROME_PATH` and `rod.Options{SlowMotion}` when debugging.
   - Use a single shared browser per test package with `sync.Once` to reduce startup cost; open a new incognito page per test.

3. **Test scenarios**
   - **Create and edit note**: load `/`, type text into `#jot-field`, wait for debounce + server write, reload page, and assert the textarea contains persisted content.
   - **Live updates**: open two pages to the same token, type in one, and assert the other receives updated text via SSE/Datastar patch.
   - **Token validation**: attempt to open `/` with an invalid `token` query and assert the server returns HTTP 400.

4. **Synchronization helpers**
   - Implement small utilities to wait for textarea value equality, debounce flush (`time.Sleep` just above 500ms or use `page.Timeout` with `Wait` on `textarea.Eval("this.value")`).
   - Add a helper to extract the token from the redirected URL or from the jot filename in `JOT_DIR` for multi-page tests.

## Developer workflow
- Document environment hints in README: how to run headless locally, how to set `ROD_HEADLESS=false` for debugging, and any required `DISPLAY` on CI.
- In CI, gate the job to run only when Chromium is available or let Rod download its bundled browser; cache the Rod download directory between runs for speed.

## Deliverables
- Rod-backed tests added to existing `_test.go` files covering the core scenarios above.
- README note explaining how to install/run the suite.
- Optional GitHub Actions job to run the Rod-backed tests in CI.
