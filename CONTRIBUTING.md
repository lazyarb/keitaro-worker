# Contributing

## Development checks

Run before submitting a change:

```sh
make verify
sh -n install-runtime.sh
scripts/test-install-runtime.sh
docker build -t lazyarb/keitaro-worker:test .
```

Keep the queue format backward-compatible. New fields must be optional for at least one major release, and upgrades must not discard `pending`, `processing`, `retry`, or `failed` events.

Pull requests from forks are reviewed before their code is run on LazyArb self-hosted infrastructure. A maintainer will place an approved commit on an organization branch to execute CI.
