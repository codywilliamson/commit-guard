# Commit-guard redesign decisions

Discussion date: 2026-09-16. Requirements for the v0.3.0 implementation. PR #1 is closed and its branch remains available as historical work.

## Accepted requirements

| Area | Decision |
| --- | --- |
| Audience | Useful across Cody's repositories and straightforward for other maintainers to adopt. |
| Primary problem | Commit messages pass locally, then fail validation after a push. |
| Consistency | Local and CI validation use the same checker version and effective policy. |
| Performance | No update lookups, downloads, or package installation during commit or pre-push checks. Batch outgoing commits into one checker invocation. |
| Header length | No default 100-character cap. Repository-specific limits may be optional. |
| Update notifications | GitHub update PRs, plus local notices when the checkout's installed setup is outdated. |
| Portability | Verify installation, hooks, and cold-start performance on Windows, macOS, and Linux. |

## Proposed update behavior

- Use Dependabot for release discovery and update PRs. It supports external GitHub action and reusable-workflow references, including commit identifiers. Merge into existing Dependabot configuration when installation configures this feature. [GitHub documentation](https://docs.github.com/en/code-security/concepts/supply-chain-security/dependabot-version-updates)
- Keep validation on the repository's selected release until its update is accepted. A new upstream release alone should not alter local or CI rules or fail an otherwise valid commit.
- After pulling an accepted update, compare the locally installed checker identity with the repository's required identity using only local data. An advisory notice should name the versions and the command that synchronizes the installation; avoid repeating advisory notices on every commit.
- If the required checker is unavailable, report that setup problem with a repair command. Do not silently validate with incompatible rules or download a replacement inside a hook.
- Provide an explicit installation/synchronization command to obtain the required release, and a status command to explain installed and required versions. Command names remain undecided.
- Version notices must remain independent of message validity: distinguish an upstream release being available from a local installation that cannot honor the repository's selected version.

## Implementation decisions

- Windows startup measurements selected Go for the shared checker. A dependency-free JavaScript adapter handles GitHub events and release downloads. See [performance measurements](../../docs/performance.md).
- The pinned external action or reusable-workflow reference is authoritative. Local checks read that same reference; `git commit-guard sync` resolves it and verifies the downloaded release checksum. No separate local pin is committed.
- New installs configure weekly Dependabot checks, preserving existing root schedules and other ecosystems. Clean installs use private native hooks; custom shell hooks retain managed wrappers.

See [Actions research](20260916-actions-best-practices-research.md) for primary sources, event-selection pitfalls, and the performance evaluation direction.
