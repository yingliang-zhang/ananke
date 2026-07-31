Working...
Implemented the provider-free preflight harness in `internal/trustedsupervisor/audit_wrapper_compatibility_test.go` only.

### Harness changes

- Constructs the complete `executionPolicyEntry`:
  - `custom:sudo`
  - `gpt-5.6-sol`
  - exactly `SUDO_API_KEY`
  - OMP `17.1.3`
  - pinned executable/root/native fixture identities
- Uses production `auditModelsConfigBytes`.
- Creates an isolated `HOME`.
- Copies the validated native fixture to:
  - `$HOME/.omp/natives/17.1.3/pi_natives.darwin-arm64.node`
  - mode `0400`
  - verified size, identity, bytes, and SHA-256 before and after execution
- Uses production validated read/write helpers.
- Adds exact native-addon read and executable-map rules plus explicit write denials.
- Retains one exact loopback TCP network allowance; no DNS, UDP, mDNS, or alternate port allowance.
- Supplies only the fixed fake credential and verifies the exact Bearer value at the local gateway.
- Requires exactly one `POST /v1/responses`.
- Rejects credential output.

No production files changed. No provider contacted. No commit made.

### Required probe result

Executed with both fixture variables and `-count=10`.

All 10 runs failed identically before making a request:

```text
requests=0
wait=signal: trace/BPT trap
stdout=""
stderr=""
native="sha256:a23bbeeb5a79a51bd12a81004e50de7cff22af8835ecec6cf381e6fd81a18f2e"
```

Captured kernel sandbox evidence included:

```text
Sandbox: omp(...) deny(1) file-read-data /System/Volumes/Preboot/Cryptexes/OS/System/Library/dyld
Sandbox: omp(...) deny(1) file-read-data /dev/dtracehelper
Sandbox: omp(...) deny(1) file-read-metadata /System/Cryptexes/OS
Sandbox: omp(...) deny(1) file-read-metadata /System/Volumes/Data
Sandbox: omp(...) deny(1) network-outbound /private/var/run/syslog
Sandbox: omp(...) deny(1) sysctl-read kern.osproductversion
Sandbox: omp(...) deny(1) sysctl-read kern.iossupportversion
Sandbox: omp(...) deny(1) sysctl-read kern.osvariant_status
Sandbox: omp(...) deny(1) sysctl-read hw.ephemeral_storage
Sandbox: omp(...) deny(1) sysctl-read hw.pagesize_compat
```

Per the explicit SIGTRAP stop condition, I did not broaden the sandbox or run the full package.
