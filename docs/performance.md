# Validation performance

Measured on Windows x64 on 2026-09-16 with Node 22.18.0, Git for Windows, and a Go 1.25.0 build targeting windows/amd64. Each case has one warm-up and 20 independent process launches. These timings include process startup and completion, rather than only the parser.

| Case | Median | p95 |
| --- | ---: | ---: |
| Check one message | 48.769 ms | 175.170 ms |
| Check 100 messages in one batch | 45.413 ms | 169.411 ms |
| Git commit with hooks disabled | 153.482 ms | 256.725 ms |
| Git commit with the native hook | 183.871 ms | 420.630 ms |

The difference between the two commit medians is 30.389 ms. This is an estimate of added overhead, not a paired per-commit measurement or a latency guarantee. Windows scheduling, filesystem scanning, and antivirus contribute substantial variability. Batch and single-message medians overlap because process startup dominates this workload.

The earlier shell compatibility hook measured 377.638 ms of additional median commit overhead on the same machine. Clean installs now use native executables directly. Repositories with custom shell hooks retain a wrapper to preserve their existing behavior and can have higher overhead.

Run `node scripts/build.mjs --current` followed by `node scripts/benchmark.mjs` to repeat the benchmark. The script uses a temporary repository and never pushes. `COMMIT_GUARD_BENCH_SAMPLES` changes the sample count.

CI uses the same native checker. Its total time additionally includes runner startup, GitHub API requests, and two concurrent release-asset downloads. It does not install npm packages or compile the checker. Local timings do not predict those network and scheduling costs; record action-step timings separately from job startup.
