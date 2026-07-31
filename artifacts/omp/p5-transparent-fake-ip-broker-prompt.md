Implement guarded transparent fake-IP support for the P5 audit HTTP gateway in `/Users/yingliangzhang/Projects/ananke-p0a-schema-codegen` with TDD. No commit. Do not edit P6 repaircontract files. No real provider call.

Observed production evidence:
- Fourth canary now correctly persisted `prepared -> waiting_for_human(provider_gateway_setup_failed)` in 9.10s.
- System DNS for the exact allowlisted provider `coding.sudoai.cc:443` returns one address: `198.18.0.34`.
- `198.18.0.0/15` is the RFC 2544 benchmark range and is rejected by `publicAuditBrokerAddress` / `auditBrokerReservedPrefixes`.
- `curl --noproxy '*' https://coding.sudoai.cc/` succeeds with `REMOTE_IP=198.18.0.34`, HTTP 200, `TLS_VERIFY=0`, proving a local transparent TUN fake-IP mapping. IPv4/IPv6 loopback listener binding succeeds.
- Do not globally treat reserved addresses as public and do not weaken TLS.

Required design:
1. Introduce an explicit address classification for audit-provider resolution: ordinary public upstream versus transparent fake-IP transport alias.
2. Accept `198.18.0.0/15` only when all are true:
   - provider + endpoint is an exact existing allowlisted route (`custom:sudo` / `coding.sudoai.cc:443` for the current route);
   - the full DNS answer set is nonempty, bounded, unique, and consists entirely of that one transport class (all public or all fake-IP; reject mixed public+fake answers);
   - no zone, malformed address, loopback, private, link-local, multicast, unspecified, documentation, CGNAT, or any other reserved range is accepted;
   - gateway upstream URL/Host/TLS `ServerName` remain the original hostname, never the fake IP;
   - dial remains exact-address pinned with `Proxy:nil`; no environment proxy, redirect, or DNS re-resolution is introduced.
3. Keep `publicAuditBrokerAddress` semantically public. Add a separately named narrow predicate for transparent fake-IP rather than deleting `198.18/15` from all reserved checks.
4. Bind provider and endpoint into address validation; a caller cannot pass fake IPs without an exact allowlisted route. If practical, restrict fake-IP support to the already Darwin-only audit execution boundary rather than broadening unrelated platforms.
5. Add TDD tests for:
   - current exact provider + `198.18.0.34` accepted and pinned;
   - both ends of `198.18/15`, just outside boundaries, malformed/zone;
   - fake IP with wrong provider/hostname/port rejected;
   - mixed public+fake rejected;
   - private, loopback, link-local, multicast, CGNAT, documentation, and other reserved addresses remain rejected;
   - exact dial cannot escape pinned fake IP;
   - TLS uses provider hostname/SNI and succeeds only with a cert valid for that hostname under an injected test root; wrong hostname/cert and redirects fail closed;
   - gateway never leaks credential headers or changes request target/body semantics.
6. Add a closed, non-secret preflight helper/test usable by the build-tagged canary to distinguish DNS fake-IP acceptance from listen failures without logging raw model data or credentials. Do not expose resolved IPs in production signed evidence unless already contractually allowed.
7. Preserve all existing broker request bounds, connection limits, timeouts, TLS minimum, no compression, no redirects, header filtering, response bounds, cleanup, and P5 policy hashes unless a schema change is truly required. Prefer a narrowly validated transport rule over policy-schema expansion.
8. Run focused broker/fake-IP/TLS tests count=10, race count=3, full trustedsupervisor single, vet, gofmt, diff-check.

Return RED/GREEN evidence and changed files. Do not create cron jobs.
