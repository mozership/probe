# Contributing to Probe

Issues, feature requests and pull requests are all welcome. To report a
vulnerability, follow [SECURITY.md](./SECURITY.md) instead of opening an
issue.

## Issues

For a bug, include the version (`probe --version`), the workflow file or the
smallest one that shows the problem, what you expected and what happened.
For a feature, tell what you want to test or monitor, and what stands in the
way today.

A bug in an external action, such as `probe-agent` or `probe-slack`, belongs
in the repository of that action.

## Development

Probe needs the Go given in [go.mod](./go.mod).

```sh
make build   # builds ./probe
make test    # runs the tests
make lint    # runs golangci-lint
go fix -diff ./...
```

CI also runs govulncheck, and `e2e/test.yml` with Probe itself, which runs
every workflow under `examples/` and `e2e/`. Some tests start servers, such
as `make http_server` and `make grpc_server`; see the [Makefile](./Makefile).

The documentation site lives in `docs/` and is built with Bun:

```sh
cd docs && bun install && bun run dev
```

## Pull Requests

- Keep a pull request to one change, and say in its description why it is
  needed as well as what it does.
- Add tests for what you change. Integration tests are named
  `*_integration_test.go`, and tests that build and start binaries
  `*_e2e_test.go`.
- When a change shows in how Probe is used, update the documentation in
  `docs/content`, in English and in Japanese (`docs/content/ja`), and the
  examples under `examples/` if they are affected.
- When a change breaks code that imports Probe as a library, or changes
  how a workflow behaves, record it under Unreleased in
  [CHANGELOG.md](./CHANGELOG.md). Other changes need no entry; the release
  notes are generated from the commits.
- Packages that code outside Probe has no need for go under `internal/`.
  External actions import only `sdk` and `sdk/markdown`.

## License

By contributing, you agree that your contributions are licensed under the
[MIT License](./LICENSE).
