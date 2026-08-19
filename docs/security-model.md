# Security model

## Protected assets

- LazyArb postback tokens in the endpoint registry.
- Keitaro sub IDs and advertising IDs in queued events.
- Delivery history in the worker log.

## Trust boundaries

The Keitaro redirect can write queue events but cannot read the endpoint registry. The restricted worker user can read the registry and queue but cannot read Keitaro application data. A host administrator remains trusted, as with any system service installed on the server.

## Network behavior

The worker only initiates HTTP GET requests to endpoint URLs installed in the registry. It follows no redirects, validates TLS, sends only `code`, `ad_id`, and `subid`, and reads at most 4 KiB of a response before closing it. It exposes no network listener.

## Runtime isolation

The systemd service uses `NoNewPrivileges`, private devices and temporary storage, a read-only system filesystem, restricted address families, and explicit read/write paths. The worker has no Docker socket, Keitaro application mount, database access, or inbound port.

## Supply chain

- Release binaries are built from tags by LazyArb self-hosted runners.
- Every binary is covered by `SHA256SUMS`.
- The checksum file has a keyless Cosign signature bundle.
- The LazyArb installer pins both the runtime installer commit and all downloaded SHA-256 digests.

External pull requests do not execute code on persistent self-hosted runners. Maintainers first review and import approved changes into an organization-owned branch.
