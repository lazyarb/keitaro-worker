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

The redirect performs no worker network call. It writes to `tmp`, flushes the file, and publishes it with same-filesystem renames. The destination redirect continues whether enqueue succeeds or fails.

## Keitaro 11 isolation

The tracker receives one read/write bind mount for the queue. A separate worker container receives:

- the queue read/write;
- the endpoint registry read-only;
- the worker log directory read/write;
- ordinary outbound bridge networking.

The worker runs as UID `65532`, with all Linux capabilities dropped, a read-only root filesystem, `no-new-privileges`, a small tmpfs, and CPU, memory, and PID limits. It does not receive host networking, the Docker socket, or Keitaro application mounts.

## Keitaro 10 fallback

The installer places a static binary under `/usr/local/libexec` and runs it as the dedicated `lazyarb-keitaro` user. Queue directories use the Keitaro redirect directory's group and the setgid bit so the redirect and worker can exchange files without world-writable permissions.

## Queue states

- `tmp`: incomplete atomic writes.
- `pending`: ready for first delivery.
- `processing`: claimed by the active worker.
- `retry`: temporary failures waiting for `next_attempt_at`.
- `failed`: invalid, permanently rejected, or retry-exhausted events.
- `state`: exclusive lock and heartbeat.

Only one worker can process a queue root. An exclusive filesystem lock prevents accidental duplicate services. On startup, interrupted `processing` events return to `pending`.

## Upgrade behavior

Worker replacement can pause delivery but cannot pause Keitaro traffic: the redirect continues appending to the persistent queue. Existing v1 events containing `endpoint` remain readable, while v2 events resolve `endpoint_id` through the protected registry.
