#!/bin/sh

set -eu

repository_root=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
temporary_root=$(mktemp -d)
trap 'rm -rf "$temporary_root"' EXIT HUP INT TERM

make_fake_commands() {
  target=$1
  mkdir -p "$target"
  cat > "$target/systemctl" <<'EOF'
#!/bin/sh
exit 0
EOF
  cat > "$target/chown" <<'EOF'
#!/bin/sh
exit 0
EOF
  cat > "$target/useradd" <<'EOF'
#!/bin/sh
exit 0
EOF
  cat > "$target/id" <<'EOF'
#!/bin/sh
case "$*" in
  '-u') printf '0\n' ;;
  '-u lazyarb-keitaro') printf '65532\n' ;;
  *) exit 0 ;;
esac
EOF
  chmod +x "$target/systemctl" "$target/chown" "$target/useradd" "$target/id"
}

test_keitaro_10() {
  root=$temporary_root/k10
  fake_bin=$root/bin
  make_fake_commands "$fake_bin"
  mkdir -p "$root/application/redirects"
  printf '#!/bin/sh\nexit 0\n' > "$root/source-worker"
  chmod +x "$root/source-worker"

  PATH="$fake_bin:$PATH" \
  LAZYARB_ALLOW_UNPRIVILEGED=1 \
  LAZYARB_KEITARO_LAYOUT=10 \
  LAZYARB_KEITARO_REDIRECT_DIR="$root/application/redirects" \
  LAZYARB_QUEUE_ROOT="$root/queue" \
  LAZYARB_CONFIG_ROOT="$root/config" \
  LAZYARB_LOG_ROOT="$root/log" \
  LAZYARB_SERVICE_FILE="$root/systemd/lazyarb-keitaro-worker.service" \
  LAZYARB_LOGROTATE_FILE="$root/logrotate/lazyarb-keitaro-worker" \
  LAZYARB_BINARY_FILE="$root/libexec/keitaro-worker" \
  LAZYARB_WORKER_BINARY_SOURCE_FILE="$root/source-worker" \
  LAZYARB_SKIP_SERVICE_START=1 \
  sh "$repository_root/install-runtime.sh" >/dev/null

  test -x "$root/libexec/keitaro-worker"
  test -d "$root/queue/state"
  grep -Fq "User=65532" "$root/systemd/lazyarb-keitaro-worker.service"
  grep -Fq "ProtectSystem=strict" "$root/systemd/lazyarb-keitaro-worker.service"
  grep -Fq "ReadOnlyPaths=$root/config" "$root/systemd/lazyarb-keitaro-worker.service"
}

test_keitaro_11() {
  root=$temporary_root/k11
  fake_bin=$root/bin
  make_fake_commands "$fake_bin"
  mkdir -p "$root/var/redirects" "$root/keitaro-env"
  state=$root/tuned
  cat > "$fake_bin/docker" <<EOF
#!/bin/sh
case "\$1" in
  ps) printf 'tracker-id registry.keitaro.io/keitaro/tracker:11.8.8\\n' ;;
  inspect) printf 'registry.keitaro.io/keitaro/tracker:11.8.8\\n' ;;
  exec)
    case "\$*" in
      *' stat -c %g '*) printf '1000\\n' ;;
      *' test -d /data/lazyarb-keitaro/pending'*) test -f '$state' ;;
      *) exit 0 ;;
    esac
    ;;
  pull) exit 0 ;;
  *) exit 0 ;;
esac
EOF
  cat > "$fake_bin/kctl" <<EOF
#!/bin/sh
touch '$state'
EOF
  chmod +x "$fake_bin/docker" "$fake_bin/kctl"

  PATH="$fake_bin:$PATH" \
  LAZYARB_ALLOW_UNPRIVILEGED=1 \
  LAZYARB_KEITARO_LAYOUT=11 \
  LAZYARB_KEITARO_REDIRECT_DIR="$root/var/redirects" \
  LAZYARB_QUEUE_ROOT="$root/queue" \
  LAZYARB_CONFIG_ROOT="$root/config" \
  LAZYARB_LOG_ROOT="$root/log" \
  LAZYARB_SERVICE_FILE="$root/systemd/lazyarb-keitaro-worker.service" \
  LAZYARB_LOGROTATE_FILE="$root/logrotate/lazyarb-keitaro-worker" \
  LAZYARB_KEITARO_VOLUME_ENV="$root/keitaro-env/tracker-traffic.local.env" \
  LAZYARB_WORKER_IMAGE='ghcr.io/lazyarb/keitaro-worker:test@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa' \
  LAZYARB_SKIP_SERVICE_START=1 \
  sh "$repository_root/install-runtime.sh" >/dev/null 2>&1

  grep -Fq -- '--read-only' "$root/systemd/lazyarb-keitaro-worker.service"
  grep -Fq -- '--cap-drop ALL' "$root/systemd/lazyarb-keitaro-worker.service"
  grep -Fq -- '--security-opt no-new-privileges' "$root/systemd/lazyarb-keitaro-worker.service"
  if grep -Fq '/var/run/docker.sock' "$root/systemd/lazyarb-keitaro-worker.service"; then
    printf 'worker service must not mount the Docker socket\n' >&2
    exit 1
  fi
  grep -Fq "$root/queue:/data/lazyarb-keitaro" "$root/keitaro-env/tracker-traffic.local.env"
}

test_keitaro_10
test_keitaro_11
printf 'runtime installer tests passed\n'
