# LazyArb Keitaro Worker

Auditable delivery worker used by the LazyArb Keitaro integration.

The custom Keitaro redirect writes a small JSON event to an atomic file queue. This worker claims queued events, delivers them to the event endpoint in bounded parallel batches, retries temporary failures, and preserves exhausted events in a dead-letter directory.

## Trust model

- The installed `worker.php` is downloaded from a pinned Git commit.
- The LazyArb installer verifies its SHA-256 digest before installation.
- The worker contains no embedded LazyArb token or workspace configuration.
- Endpoint credentials are present only in queue events created on the Keitaro host.
- The worker does not load private Keitaro classes or modify the Keitaro database.

Review [`worker.php`](worker.php) before enabling the service. The installer prints the pinned version, source URL, and checksum.

## Runtime

The worker expects these environment variables:

```text
LAZYARB_KEITARO_QUEUE_ROOT=/absolute/path/to/queue
LAZYARB_KEITARO_LOG_FILE=/absolute/path/to/worker.log
```

The queue root must contain `tmp`, `pending`, `processing`, `retry`, and `failed` directories. LazyArb's installer creates them and runs the worker as a restricted systemd service.

## Verification

```sh
sha256sum worker.php
php -l worker.php
```

Release `v1.0.0` has this digest:

```text
cf3783af7187a9e77c8a37cfd0d3f0db46a5ffe5aa24674c83cf64b3d43819f0
```

## Delivery behavior

- Up to 25 events are delivered concurrently.
- Temporary failures use bounded exponential retries.
- Events are acknowledged only after a successful HTTP response.
- Interrupted processing and incomplete queue publications are recovered.
- Events that exhaust retries remain available in `failed` for inspection.
- Operational details and delivery timing are written only to `worker.log`.

## License

MIT. See [`LICENSE`](LICENSE).
