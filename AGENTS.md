# AGENTS.md

## Project: miclaw

A lean Go port of openclaw. The original codebase exploded to 450k+ LOC. We will not repeat that mistake. This file defines how agents must operate in this repo.

## Prime Directive: Keep It Small

Every decision must favor fewer lines of code. If two approaches exist, pick the one with less code. If an abstraction doesn't pay for itself immediately, don't add it. If a type can be simpler, make it simpler. The goal is a codebase that fits in your head.

## Code Style Rules

- **All functions must fit on one page (~60 lines max).** If a function is longer, split it.
- **Every function must have at least two assert statements.** Assert preconditions, assert postconditions. Panic on violation. Do not handle gracefully — crash loud and early.
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

## Summary

Write small code. Test everything. Crash on bad state. No defensive fluff. No abstractions without immediate payoff. Reproduce bugs before fixing. Keep it lean.
