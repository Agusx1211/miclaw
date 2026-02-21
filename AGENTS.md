# AGENTS.md

## Project: miclaw

A lean Go port of openclaw. The original codebase exploded to 450k+ LOC. We will not repeat that mistake. This file defines how agents must operate in this repo.

## Prime Directive: Keep It Small

Every decision must favor fewer lines of code. If two approaches exist, pick the one with less code. If an abstraction doesn't pay for itself immediately, don't add it. If a type can be simpler, make it simpler. The goal is a codebase that fits in your head.

## Code Style Rules

- **All functions must fit on one page (~60 lines max).** If a function is longer, split it.
- **Declare data with minimal scope.** No package-level variables unless absolutely necessary. Declare variables as close to their use as possible. Prefer short-lived values.
- **Compile with ALL warnings enabled.** Use `go vet`, `staticcheck`, and `-race` in tests. Treat warnings as errors.
- **No defensive code.** Do not check for conditions that "shouldn't happen." Do not add fallbacks. Do not add nil guards on internal types. If something is nil that shouldn't be, let it panic. The tests will catch it.
- **No abstractions for the sake of abstractions.** No interfaces with a single implementation. No factory patterns. No dependency injection frameworks. No builder patterns. Pass concrete types. Call functions directly.
- **No future-proofing.** Do not add parameters "just in case." Do not add unused fields. Do not leave commented-out code. Do not write TODOs for hypothetical features. Code for what exists today, nothing more.
- **No legacy support.** This project is not live anywhere. Change types freely. Rename things freely. Break APIs freely. There are no consumers to protect.

## Testing Rules

Testing is how we ensure correctness. Not defensive code, not type gymnastics — tests.

- **TDD.** Write the test first, watch it fail, write the minimal code to pass, refactor. No exceptions.
- **Both unit and integration tests.** Unit tests for pure logic. Integration tests for anything that touches state, I/O, or multiple components together.
- **Every feature must have tests.** If it's not tested, it doesn't work.
- **Every bug fix requires a failing test FIRST.** Before touching any code to fix a bug:
  1. Write a non-trivial integration test that reproduces the bug.
  2. Run it. Confirm it fails.
  3. Only then write the fix.
  4. Confirm the test passes.
  If the bug cannot be reproduced with a test, assume the condition is unreachable and do not patch it.
- **Suspected edge cases must be replicated before handling.** Do not add handling for a theoretical edge case. Write a test that triggers it. If you can't trigger it, it doesn't exist. Move on.
- **Aim for hundreds of tests.** Correctness comes from test coverage, not from defensive code. More tests, fewer guards.

## Naming Conventions

- **Short names for short scopes.** Single-letter variables (`i`, `n`, `b`) are fine in loops and small functions. The smaller the scope, the shorter the name.
- **Descriptive names for exported symbols.** Exported functions, types, and constants get clear names. `ParseBlock`, not `PB`. `ValidateSignature`, not `ValSig`.
- **No stuttering.** A type in package `block` is `block.Header`, not `block.BlockHeader`. A function in package `tx` is `tx.Decode`, not `tx.DecodeTx`.
- **No Hungarian notation.** No `strName`, `nCount`, `bValid`. The type system handles this.
- **Acronyms are all-caps or all-lower.** `HTTPClient`, `txID`, `rpcURL`. Not `HttpClient`, `txId`, `rpcUrl`.
- **Test functions describe the scenario.** `TestDecodeRejectsEmptyInput`, not `TestDecode1`. The name should tell you what broke when the test fails.
- **Receivers are one or two letters.** `func (b *Block) Hash()`, not `func (block *Block) Hash()` or `func (self *Block) Hash()`.
- **Package names are single lowercase words.** `block`, `tx`, `codec`. Not `blockutils`, `txHelper`, `codec_v2`.
- **Files are lowercase, underscored.** `block_header.go`, `tx_decode_test.go`. No camelCase filenames.
- **Constants are PascalCase if exported, camelCase if not.** `MaxBlockSize`, `defaultTimeout`. No `ALL_CAPS_SCREAMING`.

## Dead Code Policy

Dead code is not "there if we need it later." Dead code is a liability. Delete it.

- **Never leave commented-out code.** If code is commented out, delete it. Git remembers everything; you don't need to.
- **Delete unused functions immediately.** If a refactor makes a function unreachable, delete it in the same commit. Do not leave it "for later."
- **Delete unused parameters.** If a function parameter is no longer read, remove it and update all callers.
- **Delete unused struct fields.** If a field is never set or never read, remove it.
- **Delete unused imports.** The compiler enforces this in Go, but also watch for imports only used by commented-out code.
- **Delete unused test helpers.** Test utilities rot just like production code. If no test calls it, delete it.
- **Delete stale TODO comments.** A TODO with no associated work is just noise. Either do the work now or delete the comment.
- **No "just in case" code.** If it's not called today, it shouldn't exist today. Re-creating a function from scratch when you actually need it takes less time than maintaining zombie code you might never use.
- **Every PR/commit should have a net-zero or negative line count trend.** Adding a feature should not leave behind orphaned code from the previous approach. Clean up after yourself in the same change.

## What NOT To Do

- Do not add error wrapping layers. Return errors directly.
- Do not create utility packages. Put helpers next to where they're used.
- Do not write godoc comments on internal functions unless the logic is genuinely non-obvious.
- Do not add logging "just in case." Add logging when debugging a specific problem, and consider removing it after.
- Do not second-guess existing code. If the code works and tests pass, leave it alone. Do not "improve" working code that nobody asked you to improve.
- Do not guess future usage patterns. Do not add configurability that nobody requested.
- Do not add backwards-compatibility shims, aliases, or re-exports when changing something. Just change it.
- Do not over-engineer error types. A plain `error` or `fmt.Errorf` is fine.
- Do not create interfaces preemptively. Create them only when you have two or more concrete implementations today.

## Reference Material

- `./docs/` — How our system works (our design docs).
- `./openclaw_docs/` — How the original openclaw works (reference only).
- `../openclaw/` — The original openclaw source (reference only, do not copy its patterns).

We copy openclaw's *functionality*, not its *implementation*. The original codebase is bloated. Study what it does, then write the simplest Go code that achieves the same result.

## Development Credentials

- `./DEV_VARS.md` contains development API keys for OpenRouter. This file is `.gitignored` and must stay that way.
- **Use freely for integration tests.** When running integration tests against real models, source credentials from `DEV_VARS.md`. No need to ask permission.
- **NEVER reveal credentials.** Do not commit, or include credentials in any output, error message, test fixture, or code comment.

## Summary

Write small code. Test everything. Crash on bad state. No defensive fluff. No abstractions without immediate payoff. Reproduce bugs before fixing. Keep it lean.
