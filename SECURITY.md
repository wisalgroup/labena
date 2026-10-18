# Security

## Supported versions

Bug and security fixes target only the latest published non-prerelease version;
older versions receive no backports. Maintenance is best effort, with no guaranteed
response or resolution time. Before the first release, development snapshots have
no stable support commitment. See [support and compatibility](SUPPORT.md).

## Report a vulnerability privately

The planned public-release reporting channel is GitHub private vulnerability
reporting on [wisalgroup/labena](https://github.com/wisalgroup/labena). Once enabled,
open **Security → Advisories → Report a vulnerability** there. A customized
application may have a different owner and reporting channel; report
application-specific issues to that owner.

Include the affected tag or commit, impact, reproduction steps and a minimal proof
of concept with synthetic data. Do not post credentials, personal data or
exploitable production details in public issues. If the private reporting button
is unavailable, do not substitute a public issue containing vulnerability details.

Wisal Group's repository maintainers are responsible for monitoring reports, assessing
impact, coordinating a fix and publishing an advisory with affected/fixed versions
and adoption instructions. Customized applications must incorporate those fixes
themselves.

**Channel setup is pending:** the upstream repository is currently private.
GitHub's private vulnerability reporting feature requires a public repository,
so no public reporting entry point has been enabled or verified. Before inviting
public adoption, the repository owner must enable the feature after making the
repository public, verify the reporting entry point, and ensure a maintainer
receives report notifications. Follow
[GitHub's setup instructions](https://docs.github.com/en/code-security/how-tos/report-and-fix-vulnerabilities/configure-vulnerability-reporting/configure-for-a-repository).

## Application security

Use placeholder credentials in examples. Rotate accidentally exposed credentials.
Enable authentication deliberately, restrict administrative endpoints, and review
deployment secrets and network policies before exposing an application.

Run `govulncheck ./...` with the declared Go toolchain when updating dependencies.
A passing scan does not replace authorization, integration and concurrency tests.
