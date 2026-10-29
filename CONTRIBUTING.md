# Contributing to BatoX

Thanks for your interest in BatoX! Bug reports, ideas and pull requests are welcome.

## Reporting bugs and suggesting features

Open an [issue](https://github.com/monteirohat/batox/issues/new/choose) using one of the
templates. For bugs, please include your Batocera version, your hardware (for example
"Raspberry Pi 4"), the BatoX version (shown on the dashboard), what you expected and what
happened, and the relevant lines from `./scripts/deploy.sh logs`.

Security issues should **not** be reported in public issues; see [SECURITY.md](SECURITY.md).

## Development setup

See the [development guide](docs/development.md). In short:

```bash
git clone https://github.com/monteirohat/batox.git
cd batox
make run-dev        # http://localhost:8080, no console needed
make test
```

## Pull requests

1. Fork the repository and create a branch from `main`.
2. Keep each pull request focused on one change.
3. Before pushing, run:

   ```bash
   make fmt
   make vet
   make test
   ```

4. Test on a real Batocera machine when your change touches EmulationStation, `batocera.conf`,
   the service script or the deploy script, and say so in the pull request.
5. Update the documentation in `docs/` and the `Unreleased` section of
   [CHANGELOG.md](CHANGELOG.md) when behavior changes.

## Code style

- Go code is formatted with `gofmt` and checked with `go vet`. CI fails otherwise.
- Comments and documentation are written in English.
- Comments, documentation, interface text and API messages are written in English.
- The web app has no build step: plain HTML, CSS and native ES modules. Please don't add a
  bundler or npm dependencies without discussing it in an issue first.
- Always escape dynamic text with `esc()` in the web app, and validate any client-supplied path
  on the server.

## License

By contributing, you agree that your contributions are licensed under the [MIT License](LICENSE).
