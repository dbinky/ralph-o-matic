# Contributing to RalphFactory

## Branch Naming

- Feature branches: `dev-*` (e.g., `dev-new-feature`)
- Branch from `dev`, target PRs to `dev`
- Only `dev` may open PRs to `main`

## Development Setup

1. Install Go 1.24+
2. Clone the repo and checkout `dev`
3. Run `make deps` to install dependencies
4. Run `make test` to verify everything works

## Pull Request Process

1. Create a `dev-*` branch from `dev`
2. Make your changes and add tests
3. Open a PR targeting `dev`
4. Ensure CI passes before requesting review

## Running Tests

```sh
make test       # Go unit tests
make test-bats  # BATS integration tests
make lint       # Run golangci-lint
```
