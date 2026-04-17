# Contributing

Contributions welcome! Here's how:

1. Fork the repo
2. Create a branch (`git checkout -b my-change`)
3. Make your changes
4. Test by running `bash test/test.sh`
5. Smoke-test the installer in a temp repo if you changed install behavior
6. Update `CHANGELOG.md` and `VERSION` for release-worthy changes
7. Commit using [conventional commits](https://www.conventionalcommits.org/) (of course)
8. Open a PR

## What to contribute

- New config presets
- Install script improvements (new platforms, edge cases)
- Better commit range detection in CI
- Better PR title and bot-commit handling
- Documentation fixes

## Reporting issues

Open a GitHub issue with:
- What you expected
- What happened
- Your package manager and Node.js version
