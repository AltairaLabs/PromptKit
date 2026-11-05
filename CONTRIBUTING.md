# Contributing to PromptKit

Thank you for your interest in contributing to PromptKit! This document describes how to contribute to PromptKit: the sign-off and license requirements, the development setup, and the pull request process.

## Code of Conduct

This project adheres to the [Contributor Covenant Code of Conduct](./CODE_OF_CONDUCT.md). By participating, you are expected to uphold this code. Please report unacceptable behavior to [conduct@altairalabs.ai](mailto:conduct@altairalabs.ai).

## Developer Certificate of Origin (DCO)

This project uses the Developer Certificate of Origin (DCO) to ensure that contributors have the right to submit their contributions. By making a contribution to this project, you certify that:

1. The contribution was created in whole or in part by you and you have the right to submit it under the open source license indicated in the file; or
2. The contribution is based upon previous work that, to the best of your knowledge, is covered under an appropriate open source license and you have the right under that license to submit that work with modifications, whether created in whole or in part by you, under the same open source license (unless you are permitted to submit under a different license), as indicated in the file; or
3. The contribution was provided directly to you by some other person who certified (1), (2) or (3) and you have not modified it.

### Signing your commits

Every commit needs a `Signed-off-by` trailer whose email matches the commit author. Add the `-s` flag to your commit command:

```bash
git commit -s -m "feat: add the thing"
```

This adds the trailer to your commit message:

```text
Signed-off-by: Your Name <your.email@example.com>
```

The `commit-msg` hook (installed by `./scripts/install-hooks.sh`) rejects a commit without a matching trailer. To fix the last commit:

```bash
git commit --amend -s --no-edit
```

To sign off several commits, run `git rebase --signoff <base>`.

## Contributor License Agreement (CLA)

You sign the CLA once, on your first pull request. The CLA Assistant bot comments with a link to the agreement; reply on the pull request with:

```text
I have read the CLA Document and I hereby sign the CLA
```

The CLA is a license grant, not a copyright assignment: you keep ownership of your contribution and grant AltairaLabs a license to use and relicense it. You sign once; the signature applies to your future contributions across AltairaLabs repositories. You can read the full text [here](https://gist.github.com/chaholl/acc8f1f6c38376d00a162351f566b93e).

## How to contribute

### Reporting bugs

- Check existing issues first.
- Provide clear reproduction steps.
- Include version information.
- Share relevant configuration and code samples.

### Suggesting features

- Open an issue describing the feature.
- Explain the use case and benefits.
- Discuss the implementation approach.

### Submitting changes

1. Fork the repository.
2. Create a feature branch: `git checkout -b feat/your-feature-name`.
3. Make your changes and write or update tests.
4. Run `make verify`. It runs `lint-diff` (golangci-lint on the changed modules, then gosec) and `test-fast` (`go test` on the changed modules). The pre-commit hook also builds, checks coverage, runs the weak-assertion guard and checks reference drift, and CI runs a wider set of jobs, so a passing `make verify` does not predict either.
5. Commit with `git commit -s` and a conventional-commit message (`feat:`, `fix:`, `docs:`, `chore:`). The pre-commit hook runs on every commit.
6. Push to your fork: `git push -u origin feat/your-feature-name`.
7. Open a pull request against `main`.

## Development setup

### Prerequisites

- Go, at the version `go.work` declares
- Make
- golangci-lint, at the version pinned in `.github/workflows/ci.yml`. `scripts/check-lint-version.sh` refuses any other version, because versions disagree about findings.

### Setup

```bash
# Clone your fork
git clone https://github.com/YOUR_USERNAME/PromptKit.git
cd PromptKit

# Download module dependencies
make install

# Install the git hooks (once)
./scripts/install-hooks.sh

# Build and test
make build
make test
```

The repository has no root `go.mod`. `go.work` lists the modules, so `go build ./...` and `go test ./...` fail at the repository root. The Make targets iterate the modules for you; for a single package use `go -C <module>`:

```bash
go -C runtime test ./evals/... -count=1
```

`make help` lists every target. See [Pre-commit hooks](https://promptkit.altairalabs.ai/contributors/pre-commit-hooks/) for what the hooks check.

### Project structure

```text
PromptKit/
├── runtime/          # Core runtime components
├── pkg/              # Shared packages (config)
├── sdk/              # PromptKit SDK
│   └── examples/     # Runnable SDK examples
├── server/a2a/       # A2A protocol server module
├── benchmarks/       # Framework comparison benchmarks
├── tools/            # Development tools
└── docs/             # Documentation
```

Twelve directories under `sdk/examples/` have their own `go.mod`: `audiohelper`, `duplex-streaming`, `eval-hooks`, `multitrack-ingestion`, `onnx-audio-emotion`, `openai-realtime`, `sdk-evals`, `sdk-http-auth`, `telemetry-redaction`, `topic-policy`, `voice-chat` and `voice-interview`. The other examples, such as `hello`, belong to the `sdk` module. `make build` does not compile the nested modules and `go -C sdk build ./...` does not either; CI does. Build the ones you touch:

```bash
go -C sdk/examples/sdk-evals build ./...
```

The PromptArena and PackC CLIs, and the config-file JSON schemas, live in a separate repository. See the [PromptArena docs](https://promptarena.altairalabs.ai/) to contribute to them.

## Component-specific guidelines

### SDK (`sdk/`)

The SDK is the high-level API (`Open`, `OpenDuplex`, `OpenWorkflow`). Areas for contribution:

- High-level API improvements
- Conversation patterns and middleware
- Integration helpers and utilities
- Performance optimizations
- Example applications and tutorials

```bash
# Unit tests
go -C sdk test ./...

# Integration tests (build tag)
go -C sdk test -tags=integration ./...
```

### Runtime (`runtime/`)

The runtime holds the core engine and shared components. Areas for contribution:

- Provider implementations and optimizations
- Tool execution framework
- State management and persistence
- Pipeline processing improvements
- Security and error handling

```bash
# Unit tests
go -C runtime test ./...

# Coverage report for all modules
make coverage
```

## Coding guidelines

### Go code style

- Follow standard Go conventions.
- Format with `gofmt` and `goimports`. `make lint` runs `go vet`, `go fmt` (which rewrites files), golangci-lint, gosec and the message-pattern check.
- Use clear, descriptive names.
- Add package-level documentation comments. Doc comments are published as the API reference, so keep issue numbers and inline URLs out of them.
- Keep functions focused and testable.

### Testing

- Write unit tests for new functionality.
- New and changed non-test files need at least 80% statement coverage. The pre-commit hook checks it per file, and SonarCloud checks new code in CI.
- Use table-driven tests where appropriate.
- Give tests assertions that can fail. `scripts/check-weak-assertions.sh` rejects new tests that assert only on `NoError`, `Nil` or `NotNil`.
- Mock external dependencies.
- Check `_test.go` files in the same package for name collisions before naming a type or function.

### Documentation

- Update the README when you add features.
- Add inline comments for complex logic.
- Update the example configurations you affect.
- Add package documentation for new packages.
- Pages under `docs/src/content/docs/` follow `docs/STYLE.md`. `make docs-voice` checks the mechanical rules.
- If you change an exported Go symbol or doc comment, regenerate the API reference with `make docs-reference` or `make docs-sdk-reference`. CI's reference drift check fails otherwise.

## Pull request process

1. Make CI pass. Tests, lint, build and the weak-assertion guard must succeed.
2. Update documentation: README, examples, inline docs.
3. Describe breaking changes in `CHANGELOG.md`.
4. Request review from the maintainers (see `.github/CODEOWNERS`).
5. Address feedback and resolve every review conversation.
6. Keep your branch up to date with `main`.
7. Maintainers merge with squash.

The `main` branch is protected, so you cannot push to it directly. See the [DevOps documentation](https://promptkit.altairalabs.ai/contributors/devops/) for branch protection and the CI/CD pipelines.

## Releases

Maintainers cut releases with the `release.yml` workflow. Do not create releases or tags by hand. See [DevOps](https://promptkit.altairalabs.ai/contributors/devops/#release-management).

## Questions

- Open a GitHub issue.
- Check the existing documentation.
- Review closed issues and pull requests.

## License

By contributing, you agree that your contributions will be licensed under the Apache License 2.0.
