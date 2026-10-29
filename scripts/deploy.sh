#!/usr/bin/env bash
# Deploys BatoX to a Batocera machine over SSH.
#
#   ./scripts/deploy.sh deploy   build (if needed) and install or update
#   ./scripts/deploy.sh remove   stop and remove the service and binary (keeps data)
#   ./scripts/deploy.sh status   show whether the service is running
#   ./scripts/deploy.sh logs     show the last log lines
#   ./scripts/deploy.sh token    print the API token
#
# Environment:
#   BATOCERA_HOST   target host (default: batocera.local)
#   BATOX_ARCH      arm64 (Raspberry Pi, default) or amd64 (x86_64 PC)
#
# Batocera's SSH user is root and the default password is "linux". To avoid
# typing it on every deploy, run once: ssh-copy-id root@batocera.local
set -euo pipefail

HOST="${BATOCERA_HOST:-batocera.local}"
ARCH="${BATOX_ARCH:-arm64}"
TARGET="root@${HOST}"
APP_DIR="/userdata/system/batox"
SERVICE_DIR="/userdata/system/services"
BIN="build/batox-linux-${ARCH}"

# Paths used before the project was renamed from MyMine to BatoX.
LEGACY_DIR="/userdata/system/mymine"
LEGACY_SERVICE="${SERVICE_DIR}/mymine"

cd "$(dirname "$0")/.."

# Reuse a single SSH connection, so the password is asked only once.
CTL="/tmp/batox-ssh-%r@%h:%p"
SSH_OPTS=(-o ConnectTimeout=5 -o ControlMaster=auto -o "ControlPath=${CTL}" -o ControlPersist=60)

remote() { ssh "${SSH_OPTS[@]}" "$TARGET" "$@"; }
copy() { scp "${SSH_OPTS[@]}" "$@"; }

usage() {
    sed -n '2,17p' "$0" | sed 's/^# \{0,1\}//'
    exit 1
}

# Moves an existing MyMine install to BatoX, keeping the token, database and logs.
migrate_legacy() {
    remote "
        if [ -e ${LEGACY_SERVICE} ] || [ -d ${LEGACY_DIR} ]; then
            echo '[deploy] Found a MyMine install, migrating it to BatoX...'
            batocera-services stop mymine >/dev/null 2>&1 || true
            batocera-services disable mymine >/dev/null 2>&1 || true
            rm -f ${LEGACY_SERVICE}
            if [ -d ${LEGACY_DIR} ] && [ ! -e ${APP_DIR} ]; then
                mv ${LEGACY_DIR} ${APP_DIR}
                [ -f ${APP_DIR}/mymine.db ] && mv ${APP_DIR}/mymine.db ${APP_DIR}/batox.db
                rm -f ${APP_DIR}/mymine
                echo '[deploy] Data moved to ${APP_DIR} (token and catalog kept).'
            fi
        fi"
}

case "${1:-}" in
    deploy)
        case "$ARCH" in
            arm64|amd64) ;;
            *) echo "[deploy] BATOX_ARCH must be arm64 or amd64 (got: $ARCH)"; exit 1 ;;
        esac
        if [ ! -f "$BIN" ]; then
            echo "[deploy] Binary not found, building..."
            make "build-${ARCH}"
        fi
        echo "[deploy] Connecting to ${HOST}..."
        migrate_legacy
        remote "mkdir -p ${APP_DIR} ${SERVICE_DIR}"
        remote "batocera-services stop batox >/dev/null 2>&1 || true"

        echo "[deploy] Uploading binary and service..."
        copy "$BIN" "${TARGET}:${APP_DIR}/batox.new"
        copy scripts/batox "${TARGET}:${SERVICE_DIR}/batox"
        if ! remote "test -f ${APP_DIR}/config.json"; then
            echo "[deploy] Creating default config.json..."
            copy config.example.json "${TARGET}:${APP_DIR}/config.json"
        fi

        remote "mv ${APP_DIR}/batox.new ${APP_DIR}/batox \
            && chmod +x ${APP_DIR}/batox ${SERVICE_DIR}/batox \
            && sed -i 's/\r$//' ${SERVICE_DIR}/batox \
            && batocera-services enable batox \
            && batocera-services start batox"

        sleep 2
        echo "[deploy] Checking the API..."
        if remote "curl -fsS http://127.0.0.1:8080/api/v1/ping"; then
            echo
            echo "[deploy] Done! Open http://${HOST}:8080/ - API token:"
            remote "cat ${APP_DIR}/token 2>/dev/null || echo '(token set in config.json)'"
        else
            echo "[deploy] The API did not answer. See: ./scripts/deploy.sh logs"
            exit 1
        fi
        ;;
    remove)
        remote "batocera-services stop batox >/dev/null 2>&1 || true; \
            batocera-services disable batox >/dev/null 2>&1 || true; \
            rm -f ${SERVICE_DIR}/batox ${APP_DIR}/batox"
        echo "[deploy] Service and binary removed. Data kept in ${APP_DIR}."
        ;;
    status)
        remote "${SERVICE_DIR}/batox status"
        ;;
    logs)
        remote "tail -n 40 ${APP_DIR}/logs/info.log; echo '--- errors ---'; tail -n 20 ${APP_DIR}/logs/error.log; echo '--- stdout ---'; tail -n 20 ${APP_DIR}/logs/stdout.log 2>/dev/null || true"
        ;;
    token)
        remote "cat ${APP_DIR}/token"
        ;;
    *)
        usage
        ;;
esac
