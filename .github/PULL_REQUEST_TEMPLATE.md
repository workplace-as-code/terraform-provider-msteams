## Description

<!-- Describe your changes clearly and concisely -->

## Type of Change

- [ ] Bug fix
- [ ] New resource / data source
- [ ] Enhancement to existing resource
- [ ] Documentation update
- [ ] Refactoring

## Related Issue

Fixes #<!-- issue number -->

## Testing

- [ ] Unit tests pass (`make test`)
- [ ] Acceptance tests pass against a real M365 tenant (`make testacc`)
- [ ] Documentation regenerated (`make docs`) and guides updated in `templates/guides/`

## Checklist

- [ ] Code follows project conventions
- [ ] PowerShell scripts handle errors and return valid JSON
- [ ] Go code compiles without warnings
- [ ] New resource registered in `internal/resources/registry.go` (or `specs.go` for a generic resource)
- [ ] Example added under `examples/resources/` and `make docs` run
