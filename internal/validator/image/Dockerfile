FROM debian:bookworm-slim@sha256:3783cc01769c7b2b1b83a5c5ad96c815348e28ed7da68e2e3687004faa906251

ARG WARP_VERSION=2026.8.2110.0
ENV DEBIAN_FRONTEND=noninteractive
RUN apt-get update -qq \
    && apt-get install -y --no-install-recommends ca-certificates curl gnupg \
    && curl -fsSL https://pkg.cloudflareclient.com/pubkey.gpg -o /tmp/cloudflare-pubkey.gpg \
    && gpg --batch --yes --dearmor --output /usr/share/keyrings/cloudflare-warp-archive-keyring.gpg /tmp/cloudflare-pubkey.gpg \
    && printf '%s\n' 'deb [signed-by=/usr/share/keyrings/cloudflare-warp-archive-keyring.gpg] https://pkg.cloudflareclient.com/ bookworm main' > /etc/apt/sources.list.d/cloudflare-client.list \
    && apt-get update -qq \
    && apt-get install -y --no-install-recommends "cloudflare-warp=${WARP_VERSION}" \
    && rm -rf /var/lib/apt/lists/* /tmp/cloudflare-pubkey.gpg

LABEL io.wpc.validator.protocol="2" io.wpc.validator.warp="2026.8.2110.0"
LABEL org.opencontainers.image.source="https://github.com/nguyen-duc-loc/warp-plus-connector"
COPY wpc-worker /usr/local/bin/wpc-worker
ENTRYPOINT ["/usr/local/bin/wpc-worker", "serve"]
