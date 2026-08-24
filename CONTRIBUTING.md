# Contributing

Thank you for considering a contribution.

## Before you write code

Open an issue first for anything larger than a fix. It is easier to agree on an
approach in an issue than in a pull request.

## The rules that are enforced

Everything below is checked by `dev check`, and CI runs `dev check`. Read
[AGENTS.md](AGENTS.md) for the reasoning; the short version:

1. **No comment may explain code.** Nothing inside a function body, no trailing
   comments, no commented-out code. Doc comments on declarations are expected,
   especially on exported API.
2. **95% statement coverage**, in both modules. Thresholds live in `dev.toml`.
3. **Tests need nothing installed.** No Docker, no database server, no network.
   SQLite is the test database, in memory.
4. **No Makefile, no shell scripts.** Tooling is Go, in `src/tools`.
5. **The safety layer is not refactored on a hunch.** Changing the SQL classifier
   means adding cases to the golden tables in `src/cli/pkg/sqlguard`.

## Getting set up

```bash
git clone https://github.com/sonquer/opendba
cd opendba
go run ./src/tools/cmd/dev
```

The dashboard runs every gate. `go run ./src/tools/cmd/dev check --ci` is the
non-interactive form CI uses. `golangci-lint` and `govulncheck` are pinned as
tool dependencies of `src/tools` and built into `.local/bin` on first use, so
there is nothing to install and everyone runs the same versions.

To keep your experiments out of your home directory, copy `.env.example` to
`.env` and start the program through the tooling:

```bash
cp .env.example .env && go run ./src/tools/cmd/dev run
```

## Layout

```
src/cli/     the product      module github.com/sonquer/opendba/src/cli
src/tools/   the tooling      module github.com/sonquer/opendba/src/tools
```

The generated ANTLR parsers under `src/cli/internal/parser/generated` are vendored
output, exempt from the comment and coverage gates. Regenerate them with
`go run ./src/tools/cmd/grammar`, which needs a JDK; the generated code is
committed, so nobody else needs one.

## Commits and pull requests

- [Conventional Commits](https://www.conventionalcommits.org/en/v1.0.0/):
  `feat:`, `fix:`, `docs:`, `refactor:`, `test:`, `chore:`.
- Explain *why* in the body. The diff already shows what.
- One logical change per pull request.

## Releases

The version lives in `VERSION` and follows [SemVer](https://semver.org), and
`go run ./src/tools/cmd/dev version` is what reads it.

To cut a release, run the **release-prep** workflow from the Actions tab with
`patch`, `minor`, `major`, or an exact `X.Y.Z`. It rewrites `VERSION` and opens
a pull request; review it like any other. Merging it tags `vX.Y.Z` and
`src/cli/vX.Y.Z` at the merge commit and starts the release, which runs every
gate again before publishing. The second tag is what `go install` resolves,
because the product is a nested module.

The release is started by an explicit dispatch rather than by the tag push,
because a tag pushed with `GITHUB_TOKEN` does not start a workflow. If that ever
needs to change, the drop-in alternative is `actions/create-github-app-token`:
push the tags with an App token and `release.yml` fires on the push as usual.

Nightlies build `main` every night into a single rolling prerelease under the
tag `nightly`. They are not supported, and nothing about them is a promise.

## Licence

Contributions are accepted under both the MIT and Apache-2.0 licences.
