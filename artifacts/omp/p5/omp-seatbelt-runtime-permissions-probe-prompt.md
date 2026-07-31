Systematic single-hypothesis Seatbelt probe/fix. Exact installed OMP with isolated native+models still SIGTRAP. Captured denies:
- file-read-data /System/Volumes/Preboot/Cryptexes/OS/System/Library/dyld
- file-read-data /dev/dtracehelper
- file-read-metadata /System/Cryptexes/OS
- file-read-metadata /System/Volumes/Data
- sysctl-read kern.osproductversion, kern.iossupportversion, kern.osvariant_status, hw.ephemeral_storage, hw.pagesize_compat
- network-outbound /private/var/run/syslog
Add ONLY the first four exact read/metadata paths and five exact sysctl names to the provider-free probe profile. Do NOT allow syslog, mDNS, DNS, localhost discovery ports, wildcard file/mach/network. Run exact installed probe once. If it succeeds, add the same exact rules to production auditSandboxProfile and structural tests, then run count=10/race/full package. If it still SIGTRAPs, capture only new `Sandbox: omp(...) deny` lines and stop without adding more permissions. No real provider, no commit.