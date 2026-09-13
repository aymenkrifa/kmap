# Contributing Guidelines

Thanks for your interest in contributing to kmap.

## Code of Conduct

This project follows the [Contributor Covenant](https://www.contributor-covenant.org/version/2/0/code_of_conduct.html). Please report unacceptable behaviour to the maintainer.

## Getting Started

1. Fork the repository and clone your fork.
2. Create a branch with a descriptive name (e.g. `fix/pods-selector-mismatch`, `feature/csv-output`).
3. Make your changes.
4. Push and open a pull request against `main`.

kmap requires **Go 1.26 or newer** and nothing else — no client-go, no cluster to reach, no service to stand up. It shells out to whatever `kubectl` (or wrapper) is already on your `PATH`, and the test suite never touches a real one either (see [Tests](#tests)), so `go build ./...` is all it takes to get going:

```sh
git clone https://github.com/aymenkrifa/kmap && cd kmap
go build -o ~/.local/bin/kmap .
```

## Bug Reports and Feature Requests

Open a GitHub issue. Search existing issues first to avoid duplicates. For a bug, include the `kmap` command you ran and the relevant slice of your `config.yaml` (redacted as needed) rather than just the symptom — most of what kmap does is resolve an alias against a config, so the config shape is usually half the bug report. For a feature request, describe the use case.

## Code Style

`gofmt -l .` must print nothing and `go vet ./...` must be clean. Both are enforced in CI on every push and pull request — see [`.github/workflows/ci.yml`](.github/workflows/ci.yml). Run them locally before opening a PR:

```sh
gofmt -l .
go vet ./...
```

## Tests

```sh
go test ./...
```

No test in this repo ever contacts a real cluster. Every command builds an argv and executes it through the `Runner` interface in `internal/kube`, and the test suite swaps in a `FakeRunner` that records the argv it was given and replays canned output instead — so the whole suite is hermetic, fast, and needs no kubeconfig to run.

The two most common contributions have a fixed shape:

- **Adding a subcommand** means adding a new file to `cmd/` that registers itself against `rootCmd` from its own `init()`, the way every existing command does (`cmd/pods.go`, `cmd/docs.go`, `cmd/logs.go`, and so on) — there is no central list to edit.
- **Adding a column** to `kmap pods` means adding an entry to the `columns` map in `cmd/columns.go`: a header, the extra dataset it needs (if any — `metrics`, `deployments` or `ingresses`), and a `render` function. `--columns` and `defaults.columns` pick it up automatically once it's in the map.

## Pull Requests

Keep PRs focused on a single change and describe the motivation in the PR body. If the change affects how kmap talks to a cluster (a new kubectl call, a new flag that reaches kubectl untouched), say so explicitly — that's the part a reviewer can't infer from a diff of Go code alone.

## Contact

For questions, email the maintainer at [aymenkrifa@gmail.com](mailto:aymenkrifa@gmail.com).
