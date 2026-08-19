# Architecture

## Request path

```text
Keitaro click pipeline
        |
        | atomic local file write
        v
/var/lib/lazyarb-keitaro/pending
        |
        | asynchronous claim and delivery
        v
LazyArb worker -> LazyArb postback endpoint
```

The redirect performs no network call to the worker. It writes to `tmp`, flushes the file, and publishes it with a same-filesystem rename. The destination redirect continues whether enqueue succeeds or fails.

## Runtime

Both supported Keitaro versions run the same static binary from `/usr/local/libexec/lazyarb-keitaro-worker` under the dedicated `lazyarb-keitaro` system user. A hardened systemd unit grants write access only to the queue and log directories and read access to the endpoint registry.

For Keitaro 11, the tracker container receives `/var/lib/lazyarb-keitaro` as a read/write bind mount at `/data/lazyarb-keitaro`. Docker is used only while installing or validating that mount. The worker remains a host service and has no Docker socket access.

For Keitaro 10, the redirect writes directly to the host queue. Setgid queue directories preserve the Keitaro redirect group so the redirect and worker can exchange files without world-writable permissions.

## Queue states

- `tmp`: incomplete atomic writes.
- `pending`: ready for first delivery.
- `processing`: claimed by the worker.
- `retry`: temporary failures waiting for `next_attempt_at`.
- `failed`: invalid, permanently rejected, or retry-exhausted events.
- `state`: exclusive lock and heartbeat.

Only one worker can process a queue root. An exclusive filesystem lock prevents duplicate processing services. On startup, interrupted `processing` events return to `pending`.

Replacing the worker can pause delivery but cannot pause Keitaro traffic: the redirect continues appending to the persistent queue. Queue events contain an endpoint ID; the protected registry resolves it to the LazyArb postback URL.
