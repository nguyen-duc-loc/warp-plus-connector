#!/bin/sh
# Exercise local worker readiness and cancellation without provider access.
set -eu

if [ "$#" -ne 1 ]; then
    printf 'usage: sh scripts/smoke-validator-image.sh <immutable-image-id>\n' >&2
    exit 1
fi
if ! printf '%s\n' "$1" | grep -Eq '^sha256:[0-9a-f]{64}$'; then
    printf 'An immutable sha256 image ID is required.\n' >&2
    exit 1
fi

container_id=$(docker run -d --pull=never --network none --memory 512m --cpus 1 --pids-limit 256 "$1")
trap 'docker rm -f "$container_id" >/dev/null' 0
trap 'exit 130' INT
trap 'exit 143' TERM

attempt=0
until docker exec "$container_id" /usr/local/bin/wpc-worker ready >/dev/null 2>&1; do
    attempt=$((attempt + 1))
    if [ "$attempt" -ge 20 ]; then
        printf 'Worker did not become ready within the smoke check budget.\n' >&2
        exit 1
    fi
    sleep 1
done

prepared=$(docker exec -i "$container_id" /usr/local/bin/wpc-worker initialize <<'JSON'
{"session_id":"OfflineReleaseSmokeTest","key":"SYNTHETI-CKEY0000-00000000"}
JSON
)
printf '%s\n' "$prepared" | grep -q '"phase":"prepared"'
docker exec "$container_id" /usr/local/bin/wpc-worker cancel >/dev/null
cleaned=$(docker exec -i "$container_id" /usr/local/bin/wpc-worker recover <<'JSON'
{"remaining_us":10000000}
JSON
)
printf '%s\n' "$cleaned" | grep -q '"phase":"cleaned"'
printf 'Worker readiness, initialization, and local cancellation/recovery passed (network disabled).\n'
