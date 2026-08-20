#!/bin/sh

set -eu

runtime_version=1.1.0

keitaro_layout=${LAZYARB_KEITARO_LAYOUT:-}
redirect_dir=${LAZYARB_KEITARO_REDIRECT_DIR:-}
queue_root=${LAZYARB_QUEUE_ROOT:-}
config_root=${LAZYARB_CONFIG_ROOT:-/etc/lazyarb-keitaro}
log_root=${LAZYARB_LOG_ROOT:-/var/log/lazyarb-keitaro}
service_file=${LAZYARB_SERVICE_FILE:-/etc/systemd/system/lazyarb-keitaro-worker.service}
logrotate_file=${LAZYARB_LOGROTATE_FILE:-/etc/logrotate.d/lazyarb-keitaro-worker}
binary_file=${LAZYARB_BINARY_FILE:-/usr/local/libexec/lazyarb-keitaro-worker}
worker_binary_url=${LAZYARB_WORKER_BINARY_URL:-}
worker_binary_sha256=${LAZYARB_WORKER_BINARY_SHA256:-}
worker_binary_source=${LAZYARB_WORKER_BINARY_SOURCE_FILE:-}
skip_service_start=${LAZYARB_SKIP_SERVICE_START:-0}

fail() {
  printf 'LazyArb worker installer: %s\n' "$1" >&2
  exit 1
}

require_command() {
  command -v "$1" >/dev/null 2>&1 || fail "$1 is required"
}

validate_path() {
  case "$2" in /*) ;; *) fail "$1 must be an absolute path" ;; esac
  case "$2" in *[!A-Za-z0-9_./-]*) fail "$1 contains unsupported characters" ;; esac
}

if [ "${LAZYARB_ALLOW_UNPRIVILEGED:-0}" != 1 ] && [ "$(id -u)" -ne 0 ]; then
  fail 'run this installer as root'
fi
require_command sha256sum
require_command stat
require_command systemctl
require_command find

if [ -z "$redirect_dir" ]; then
  if [ -d /var/www/keitaro/var/redirects ]; then
    redirect_dir=/var/www/keitaro/var/redirects
  elif [ -d /var/www/keitaro/application/redirects ]; then
    redirect_dir=/var/www/keitaro/application/redirects
  elif [ -d /data/var/redirects ]; then
    redirect_dir=/data/var/redirects
  fi
fi
[ -n "$redirect_dir" ] && [ -d "$redirect_dir" ] || fail 'Keitaro redirects directory was not found'

if [ -z "$keitaro_layout" ]; then
  case "$redirect_dir" in
    */var/redirects|/data/var/redirects) keitaro_layout=11 ;;
    */application/redirects) keitaro_layout=10 ;;
    *) fail 'could not detect Keitaro layout; set LAZYARB_KEITARO_LAYOUT to 10 or 11' ;;
  esac
fi
case "$keitaro_layout" in 10|11) ;; *) fail 'LAZYARB_KEITARO_LAYOUT must be 10 or 11' ;; esac

if [ -z "$queue_root" ]; then
  case "$keitaro_layout" in
    11)
      keitaro_root=${redirect_dir%/var/redirects}
      [ "$keitaro_root" != "$redirect_dir" ] || fail 'could not derive Keitaro 11 root from redirects directory'
      queue_root=$keitaro_root/var/lazyarb-keitaro
      ;;
    10) queue_root=/var/lib/lazyarb-keitaro ;;
  esac
fi

validate_path 'queue root' "$queue_root"
validate_path 'config root' "$config_root"
validate_path 'log root' "$log_root"
validate_path 'service file' "$service_file"
validate_path 'logrotate file' "$logrotate_file"
validate_path 'binary file' "$binary_file"
validate_path 'redirect directory' "$redirect_dir"

mkdir -p "$queue_root/tmp" "$queue_root/pending" "$queue_root/processing" "$queue_root/retry" "$queue_root/failed" "$queue_root/state"
mkdir -p "$config_root/endpoints.d" "$log_root" "$(dirname "$service_file")" "$(dirname "$logrotate_file")"
touch "$log_root/worker.log"

require_command useradd
if ! id lazyarb-keitaro >/dev/null 2>&1; then
  useradd --system --user-group --home-dir /nonexistent --shell /usr/sbin/nologin lazyarb-keitaro
fi
worker_uid=$(id -u lazyarb-keitaro)
worker_gid=$(id -g lazyarb-keitaro)

queue_gid=$(stat -c %g "$redirect_dir")
case "$queue_gid" in ''|*[!0-9]*) fail 'Keitaro queue group is invalid' ;; esac

chown "$worker_uid:$queue_gid" "$queue_root" "$queue_root/tmp" "$queue_root/pending" "$queue_root/processing" "$queue_root/retry" "$queue_root/failed" "$queue_root/state"
chmod 2770 "$queue_root" "$queue_root/tmp" "$queue_root/pending" "$queue_root/processing" "$queue_root/retry" "$queue_root/failed" "$queue_root/state"
chown -R "root:$worker_gid" "$config_root"
chmod 0750 "$config_root" "$config_root/endpoints.d"
find "$config_root/endpoints.d" -maxdepth 1 -type f -name '*.url' -exec chown "root:$worker_gid" {} \; -exec chmod 0640 {} \;
chown -R "$worker_uid:$worker_gid" "$log_root"
chmod 0700 "$log_root"
chmod 0640 "$log_root/worker.log"

mkdir -p "$(dirname "$binary_file")"
binary_temporary=$(mktemp "$(dirname "$binary_file")/.keitaro-worker.XXXXXX")
trap 'rm -f "${binary_temporary:-}"' EXIT HUP INT TERM
if [ -n "$worker_binary_source" ]; then
  [ -f "$worker_binary_source" ] || fail 'configured worker binary was not found'
  cp "$worker_binary_source" "$binary_temporary"
else
  [ -n "$worker_binary_url" ] || fail 'LAZYARB_WORKER_BINARY_URL is required'
  [ -n "$worker_binary_sha256" ] || fail 'LAZYARB_WORKER_BINARY_SHA256 is required'
  require_command curl
  curl --proto '=https' --tlsv1.2 -fsSL "$worker_binary_url" -o "$binary_temporary" || fail 'could not download the worker binary'
fi
if [ -n "$worker_binary_sha256" ]; then
  actual_sha256=$(sha256sum "$binary_temporary" | awk '{print $1}')
  [ "$actual_sha256" = "$worker_binary_sha256" ] || fail 'worker binary checksum verification failed'
fi
chown root:root "$binary_temporary"
chmod 0755 "$binary_temporary"
"$binary_temporary" version >/dev/null || fail 'worker binary could not execute on this host'
mv -f "$binary_temporary" "$binary_file"
binary_temporary=
trap - EXIT HUP INT TERM

cat > "$service_file" <<EOF
[Unit]
Description=LazyArb Keitaro postback worker
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=$worker_uid
Group=$queue_gid
SupplementaryGroups=$worker_gid
Environment="LAZYARB_QUEUE_ROOT=$queue_root"
Environment="LAZYARB_ENDPOINTS_ROOT=$config_root/endpoints.d"
Environment="LAZYARB_LOG_FILE=$log_root/worker.log"
ExecStart=$binary_file run
Restart=always
RestartSec=2
UMask=0007
NoNewPrivileges=true
PrivateDevices=true
PrivateTmp=true
ProtectHome=true
ProtectSystem=strict
ReadWritePaths=$queue_root $log_root
ReadOnlyPaths=$config_root
RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6
RestrictSUIDSGID=true
LockPersonality=true
MemoryDenyWriteExecute=true

[Install]
WantedBy=multi-user.target
EOF

cat > "$logrotate_file" <<EOF
$log_root/worker.log {
    daily
    rotate 14
    maxsize 20M
    missingok
    notifempty
    compress
    delaycompress
}
EOF

systemctl daemon-reload
systemctl enable lazyarb-keitaro-worker.service >/dev/null
if [ "$skip_service_start" = 1 ]; then
  printf 'Worker service was installed but not started.\n'
else
  systemctl restart lazyarb-keitaro-worker.service || fail 'worker service did not start'
  systemctl is-active --quiet lazyarb-keitaro-worker.service || fail 'worker service is not active'
fi

printf 'LazyArb worker runtime %s installed for Keitaro %s.\n' "$runtime_version" "$keitaro_layout"
printf 'Queue: %s\n' "$queue_root"
printf 'Endpoint registry: %s/endpoints.d\n' "$config_root"
printf 'Log: %s/worker.log\n' "$log_root"
printf 'Status: systemctl status lazyarb-keitaro-worker.service\n'
