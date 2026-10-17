# Security Policy

## Supported versions

`singbox-exporter` is not yet tagged with releases. Treat the latest commit on
the default branch as the only supported version and pin it when deploying.

## Reporting a vulnerability

Please do **not** open a public issue for security problems. Instead, report
them privately:

- If a GitHub security advisory workflow is enabled for this repository, use the
  *Report a vulnerability* button on the repository's *Security* tab.
- Otherwise, contact the maintainer directly by e-mail or private message and
  include the word `SECURITY` in the subject.

Please include in your report:

- the affected version (commit hash or tag),
- a short description of the issue,
- the steps to reproduce and, if possible, a proof of concept,
- the impact you observed (what an attacker could do).

We will acknowledge the report within 3 business days and aim for a fix and a
coordinated disclosure. Please allow us time to release a fix before making the
issue public.

## Notes on the threat model

This exporter is designed to run on the **same host** as sing-box and to read
loopback-only data. Practical hardening guidance:

- bind the sing-box Clash API to `127.0.0.1` and set a `secret`; pass the token
  with `-token`,
- bind the exporter port to an interface reachable only by Prometheus,
- do not expose `/metrics` to untrusted networks (label values are escaped,
  but the whole surface is waste you do not need on the Internet).