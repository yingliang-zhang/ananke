# Security Policy

## Reporting a Vulnerability

If you discover a security vulnerability in Ananke, please report it
responsibly:

1. **Do not open a public GitHub issue.**
2. Email: zhangyingliang@outlook.com
3. Include a description of the vulnerability and, if possible, a
   reproduction steps or proof-of-concept.

You will receive a response within 72 hours. Please do not disclose the
vulnerability publicly until a fix is released.

## Scope

Ananke is a single-user local application. The following are considered
security-relevant:

- Process isolation and lifecycle (orphan/leak prevention)
- Local socket and IPC security
- Credential handling (API keys, tokens)
- SQLite database integrity

The following are explicitly out of scope for the current single-user
threat model:

- Remote/network attacks against a multi-user deployment
- Same-user privilege escalation (the data root is trusted)
- Denial of service through local resource exhaustion
