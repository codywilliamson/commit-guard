# Configuration reference

`commit-guard` reads one repository policy file: `.commit-guard.json`. The action reads that file from the event head through the GitHub Contents API, while the local checker reads it from the repository root. Both paths parse strict JSON and reject unknown fields, malformed values, and trailing data.

## Policy fields

| Field | Type | Default | Effect |
| --- | --- | --- | --- |
| `types` | array of strings | `build`, `chore`, `ci`, `docs`, `feat`, `fix`, `perf`, `refactor`, `revert`, `style`, `test` | Allowed commit types. Matching ignores case. Each value must be unique, non-empty, and contain no spaces or header punctuation. |
| `maxHeaderLength` | integer | `0` | Maximum number of characters in the first line. `0` means unlimited. Negative values fail configuration parsing. |
| `ignoreMergeCommits` | boolean | `true` | Skip headers beginning with `Merge ` or equal to `Merge`. |
| `ignoreFixupCommits` | boolean | `true` | Skip headers beginning with `fixup! ` or `squash! `. |

The default policy is equivalent to:

```json
{}
```

To allow only a smaller set and cap the header at 72 characters:

```json
{
  "types": ["feat", "fix", "docs"],
  "maxHeaderLength": 72,
  "ignoreMergeCommits": true,
  "ignoreFixupCommits": true
}
```

The first line must have this shape:

```text
type(scope)!: description
```

The scope and `!` marker are optional. The description must be non-empty. Revert headers are accepted as well as the configured types.

## Strict parsing failures

The checker exits with a runtime error when the policy is an array, uses an unknown field, repeats a type, contains invalid JSON, or has data after the JSON object. For example, this fails because `headerLimit` is not a supported field:

```json
{
  "headerLimit": 72
}
```

An absent `.commit-guard.json` is different from a malformed one: absence selects the default policy, while malformed or inaccessible configuration stops validation. The action treats a GitHub Contents API 404 as absent and reports other API or decoding failures.

## Local and action parity

Keep the file at the repository root and commit it with the workflow. Local hooks use the installed checker's copy. The action resolves the copy at the event head, so a pull request is checked with the policy represented by its head commit. The action reads the fixed `.commit-guard.json` path used locally.

The policy is data only. JavaScript configuration, executable configuration hooks, presets, message ignore patterns, and package-manager-specific options are not read.
