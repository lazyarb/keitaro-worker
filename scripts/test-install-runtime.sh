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
  '-g lazyarb-keitaro') printf '65533\n' ;;
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
  grep -Fq "SupplementaryGroups=65533" "$root/systemd/lazyarb-keitaro-worker.service"
  grep -Fq "ProtectSystem=strict" "$root/systemd/lazyarb-keitaro-worker.service"
  grep -Fq "ReadOnlyPaths=$root/config" "$root/systemd/lazyarb-keitaro-worker.service"
}

test_keitaro_11() {
  root=$temporary_root/k11
  fake_bin=$root/bin
  make_fake_commands "$fake_bin"
  mkdir -p "$root/var/redirects"
  printf '#!/bin/sh\nexit 0\n' > "$root/source-worker"
  chmod +x "$root/source-worker"

  PATH="$fake_bin:$PATH" \
  LAZYARB_ALLOW_UNPRIVILEGED=1 \
  LAZYARB_KEITARO_LAYOUT=11 \
  LAZYARB_KEITARO_REDIRECT_DIR="$root/var/redirects" \
  LAZYARB_CONFIG_ROOT="$root/config" \
  LAZYARB_LOG_ROOT="$root/log" \
  LAZYARB_SERVICE_FILE="$root/systemd/lazyarb-keitaro-worker.service" \
  LAZYARB_LOGROTATE_FILE="$root/logrotate/lazyarb-keitaro-worker" \
  LAZYARB_BINARY_FILE="$root/libexec/keitaro-worker" \
  LAZYARB_WORKER_BINARY_SOURCE_FILE="$root/source-worker" \
  LAZYARB_SKIP_SERVICE_START=1 \
  sh "$repository_root/install-runtime.sh" >/dev/null 2>&1

  test -x "$root/libexec/keitaro-worker"
  grep -Fq "User=65532" "$root/systemd/lazyarb-keitaro-worker.service"
  grep -Fq "SupplementaryGroups=65533" "$root/systemd/lazyarb-keitaro-worker.service"
  grep -Fq "ExecStart=$root/libexec/keitaro-worker run" "$root/systemd/lazyarb-keitaro-worker.service"
  grep -Fq "Environment=\"LAZYARB_QUEUE_ROOT=$root/var/lazyarb-keitaro\"" "$root/systemd/lazyarb-keitaro-worker.service"
  grep -Fq "ProtectSystem=strict" "$root/systemd/lazyarb-keitaro-worker.service"
  test -d "$root/var/lazyarb-keitaro/pending"

  PATH="$fake_bin:$PATH" \
  LAZYARB_ALLOW_UNPRIVILEGED=1 \
  LAZYARB_KEITARO_LAYOUT=11 \
  LAZYARB_KEITARO_REDIRECT_DIR="$root/var/redirects" \
  LAZYARB_CONFIG_ROOT="$root/config" \
  LAZYARB_LOG_ROOT="$root/log" \
  LAZYARB_SERVICE_FILE="$root/systemd/lazyarb-keitaro-worker.service" \
  LAZYARB_LOGROTATE_FILE="$root/logrotate/lazyarb-keitaro-worker" \
  LAZYARB_BINARY_FILE="$root/libexec/keitaro-worker" \
  LAZYARB_WORKER_BINARY_SOURCE_FILE="$root/source-worker" \
  LAZYARB_SKIP_SERVICE_START=1 \
  sh "$repository_root/install-runtime.sh" >/dev/null 2>&1

  if grep -Eq 'docker|podman|kctl|TRACKER_TRAFFIC_VOLUMES' "$repository_root/install-runtime.sh"; then
    printf 'runtime installer must not depend on the Keitaro container layout\n' >&2
    exit 1
  fi
}

test_common_service_model() {
  for directive in \
    'Type=simple' \
    'NoNewPrivileges=true' \
    'PrivateDevices=true' \
    'PrivateTmp=true' \
    'ProtectHome=true' \
    'ProtectSystem=strict' \
    'RestrictSUIDSGID=true' \
    'LockPersonality=true' \
    'MemoryDenyWriteExecute=true'
  do
    grep -Fq "$directive" "$temporary_root/k10/systemd/lazyarb-keitaro-worker.service"
    grep -Fq "$directive" "$temporary_root/k11/systemd/lazyarb-keitaro-worker.service"
  done
}

test_keitaro_10
test_keitaro_11
test_common_service_model
printf 'runtime installer tests passed\n'
