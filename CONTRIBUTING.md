# Contributing

Thanks for helping improve Containerd UI.

## Development setup

1. Install Go 1.26 or newer.
2. Install WSL2 and required container tooling if you plan to run the app locally.
3. Clone the repository.
4. Run:

```bash
go mod download
go test ./...
```

## Pull requests

- Keep changes focused and easy to review.
- Prefer small, well-documented updates.
- Add or update tests where relevant.
- Ensure the project still builds successfully.

## Coding conventions

- Follow standard Go formatting (`gofmt`).
- Prefer clear errors and explicit user-facing messages.
- Keep platform-specific logic isolated where possible.

## Issue reports

Use GitHub issues for bugs, feature requests, and improvement ideas. Include steps to reproduce, environment details, and expected vs. actual behavior.
