# Security policy

Report vulnerabilities through GitHub private security advisories for this repository. Do not include production tokens, endpoint files, queue events, logs, or postback URLs in public issues.

The latest GitHub release is supported. Release binaries have SHA-256 checksums and a keyless Cosign signature bundle. Production installers pin both the runtime installer checksum and the platform binary checksum.

The worker deliberately does not:

- run in Docker or mount the Docker socket;
- read Keitaro databases or private application classes;
- accept endpoint URLs from queue events;
- log endpoint URLs, tokens, sub IDs, or response bodies;
- expose a network listener.
