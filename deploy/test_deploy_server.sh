#!/usr/bin/env bash
# Isolated success and rollback tests. No real Qdrant/Ollama/systemd operations.
set -Eeuo pipefail
umask 077
PROJECT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TEST_ROOT="$(mktemp -d /tmp/hive-atlas-deploy-test.XXXXXX)"
trap 'rm -rf -- "$TEST_ROOT"' EXIT
BUNDLE="$TEST_ROOT/bundle"
mkdir -p "$BUNDLE/deploy" "$BUNDLE/bin" "$BUNDLE/signals" "$TEST_ROOT/private" "$TEST_ROOT/fake-bin" "$TEST_ROOT/systemd"
cp "$PROJECT_ROOT/deploy/deploy_server.sh" "$PROJECT_ROOT/deploy/atlas.env.example" "$PROJECT_ROOT/deploy/hive-atlas.service" "$BUNDLE/deploy/"
cp "$PROJECT_ROOT/signals/core.json" "$BUNDLE/signals/"
printf '%040d\n' 1 > "$BUNDLE/REVISION"
printf 'synthetic-admin-key\n' > "$TEST_ROOT/private/admin.key"
: > "$TEST_ROOT/private/ca.crt"
cat > "$BUNDLE/bin/hive-atlas" <<'SH'
#!/usr/bin/env bash
printf '%s\n' "$1" >> "$HIVE_TEST_COMMAND_LOG"
case "$1" in
  version) printf '{"version":"%s","revision":"%040d","runtime":"go"}\n' "$HIVE_TEST_VERSION" 1 ;;
  push) [ "${HIVE_TEST_PUSH_FAIL:-0}" = 0 ] || exit 1 ;;
  mint-reader) printf 'synthetic-reader-token\n' > "$3" ;;
  *) exit 1 ;;
esac
SH
cat > "$TEST_ROOT/fake-bin/systemctl" <<'SH'
#!/usr/bin/env bash
printf '%s\n' "$*" >> "$HIVE_TEST_SERVICE_LOG"
SH
cat > "$TEST_ROOT/fake-bin/curl" <<'SH'
#!/usr/bin/env bash
printf '{"status":"ok","version":"%s","signals":3}\n' "${HIVE_TEST_HEALTH_VERSION:-$HIVE_TEST_VERSION}"
SH
chmod 0755 "$BUNDLE/bin/hive-atlas" "$TEST_ROOT/fake-bin/systemctl" "$TEST_ROOT/fake-bin/curl"
export PATH="$TEST_ROOT/fake-bin:$PATH"
export HIVE_TEST_COMMAND_LOG="$TEST_ROOT/commands.log" HIVE_TEST_SERVICE_LOG="$TEST_ROOT/service.log"
export HIVE_ATLAS_DEPLOY_TEST_MODE=1 HIVE_ATLAS_INSTALL_ROOT="$TEST_ROOT/install" HIVE_ATLAS_PRIVATE_ROOT="$TEST_ROOT/private"
export HIVE_ATLAS_SYSTEMD_ROOT="$TEST_ROOT/systemd" HIVE_ATLAS_HISTORY_DIR="$TEST_ROOT/history"
export HIVE_TEST_VERSION=v9.9.9
bash "$BUNDLE/deploy/deploy_server.sh" --version "$HIVE_TEST_VERSION"
CURRENT="$TEST_ROOT/install/current"
EXPECTED="$TEST_ROOT/install/releases/v9.9.9"
[ "$(readlink "$CURRENT")" = "$EXPECTED" ]
[ -x "$EXPECTED/bin/hive-atlas" ]
[ -f "$EXPECTED/deploy/reader.key" ]
grep -q 'atlas_signals_v01_v9_9_9_000000000000' "$EXPECTED/deploy/service.env"
grep -q '"result":"healthy"' "$TEST_ROOT/history/history.jsonl"

# A writer failure occurs before promotion and leaves the old API/library active.
export HIVE_TEST_VERSION=v9.9.10 HIVE_TEST_PUSH_FAIL=1
if bash "$BUNDLE/deploy/deploy_server.sh" --version "$HIVE_TEST_VERSION"; then echo 'writer failure was accepted' >&2; exit 1; fi
[ "$(readlink "$CURRENT")" = "$EXPECTED" ]
# An unhealthy new service restores the previous symlink and its unit/env/reader key.
export HIVE_TEST_PUSH_FAIL=0 HIVE_TEST_HEALTH_VERSION=v9.9.9
if bash "$BUNDLE/deploy/deploy_server.sh" --version "$HIVE_TEST_VERSION"; then echo 'unhealthy service was accepted' >&2; exit 1; fi
[ "$(readlink "$CURRENT")" = "$EXPECTED" ]
[ "$(wc -l < "$TEST_ROOT/history/history.jsonl")" = 1 ]
[ "$(grep -c '^restart hive-atlas.service$' "$HIVE_TEST_SERVICE_LOG")" = 3 ]
printf 'OK: native deploy, immutable snapshot and failed-push/health rollback\n'
