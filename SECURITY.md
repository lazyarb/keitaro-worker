# Security policy

Report vulnerabilities through GitHub private security advisories for this repository. Do not include production tokens, endpoint files, queue events, logs, or postback URLs in public issues.

Supported releases are the latest `v2.x` release and any newer major release listed in GitHub Releases. Security fixes are not backported to the legacy PHP `v1.x` worker.

Release images are published with BuildKit provenance and SBOM attestations and signed with GitHub Actions keyless identity. Production installers pin both the runtime installer checksum and image digest.

The worker deliberately does not:

- mount the Docker socket;
- read Keitaro databases or private application classes;
- accept arbitrary endpoint URLs from v2 queue events;
- log endpoint URLs, tokens, sub IDs, or response bodies;
- expose a network listener.

Legacy v1 queue events can contain an endpoint URL so that upgrades do not lose pending delivery. They are accepted only during migration and disappear as the old queue drains.
