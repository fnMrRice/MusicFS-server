# Build context must be a Linux installation bundle, not the source checkout.
# Pin BASE_IMAGE to a reviewed digest when promoting an image.
ARG BASE_IMAGE=debian:bookworm-slim
FROM ${BASE_IMAGE}
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates curl ffmpeg \
    && rm -rf /var/lib/apt/lists/* \
    && mkdir -p /var/lib/musicfs/data /var/lib/musicfs/media /var/cache/musicfs /etc/musicfs /music \
    && chown -R 10001:10001 /var/lib/musicfs /var/cache/musicfs /etc/musicfs
COPY --chmod=0555 bin/ /opt/musicfs/bin/
COPY --chmod=0444 LICENSE release.json SHA256SUMS /opt/musicfs/
COPY --chown=10001:10001 licenses/ /opt/musicfs/licenses/
COPY --chown=10001:10001 docs/ /opt/musicfs/docs/
USER 10001:10001
WORKDIR /opt/musicfs
ENV MUSICFS_HEALTH_URL=https://localhost:8443/api/v1/health/ready
ENV MUSICFS_HEALTH_CA=/etc/musicfs/tls/ca.pem
HEALTHCHECK --interval=30s --timeout=5s --start-period=30s --retries=3 \
    CMD curl --fail --silent --max-time 4 --cacert "$MUSICFS_HEALTH_CA" "$MUSICFS_HEALTH_URL" > /dev/null || exit 1
STOPSIGNAL SIGTERM
EXPOSE 8443
ENTRYPOINT ["/opt/musicfs/bin/musicfs"]
CMD ["serve", "--config", "/etc/musicfs/server.yaml"]
