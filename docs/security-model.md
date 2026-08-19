# Security model

## Protected assets

- LazyArb postback token in the endpoint registry.
- Keitaro sub IDs and advertising IDs in queued events.
- Delivery history in the worker log.

## Trust boundaries

The Keitaro tracker is allowed to write queue events but cannot read the v2 endpoint registry. The worker can read the registry and queue but cannot read Keitaro application data. A host administrator can access both and remains trusted, as with any system service installed on the Keitaro server.

## Network behavior

The worker only initiates HTTP GET requests to endpoint URLs installed in the registry. It follows no redirects, validates TLS, sends only `code`, `ad_id`, and `subid`, and reads at most 4 KiB of a response before closing it. It exposes no port.

## Supply chain

- Builder images are pinned by digest.
- Release images and binaries are built from tags by LazyArb self-hosted runners.
- Images include SBOM and provenance attestations and are signed with Cosign keyless identity.
- The LazyArb application pins the runtime installer by commit and checksum and the image by registry digest.

External pull requests do not execute code on persistent self-hosted runners. Maintainers first review and import the change into an organization-owned branch.
