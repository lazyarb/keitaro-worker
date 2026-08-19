# Contributing

## Development checks

Run before submitting a change:

```sh
make verify
make build
dist/keitaro-worker version
sh -n install-runtime.sh
scripts/test-install-runtime.sh
```

Keep the queue format backward-compatible within a supported major release. Upgrades must not discard `pending`, `processing`, `retry`, or `failed` events.

Pull requests from forks are reviewed before their code is run on LazyArb self-hosted infrastructure. A maintainer places an approved commit on an organization branch to execute CI.
