# Security Policy

## Reporting a vulnerability

Please report security issues privately by email to:

soulbialogur@gmail.com

Do not open public issues for security-sensitive problems before a fix is prepared and responsibly disclosed.

## Scope of support

The project currently supports:

- the current main branch
- the latest published release tag

Older revisions are not guaranteed to receive security fixes.

## Response expectations

We aim to acknowledge valid reports within 5 business days and work toward a fix as quickly as possible. If a fix requires coordination or a staged disclosure, we will coordinate with the reporter before public disclosure.

## Security notes for contributors

- Keep credentials, tokens, and local environment secrets out of commits and issue reports.
- Treat generated configuration files as sensitive data.
- Validate new dependencies and config changes before release.
- Use the project release checklist to confirm that the shipping binary does not expose internal debug settings or credentials in production builds.

## Disclosure timeline

Security fixes will be handled in the most responsible and timely manner possible, with public disclosure coordinated with the reporter when appropriate.
