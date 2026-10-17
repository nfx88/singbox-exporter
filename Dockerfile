FROM alpine:3.22
COPY singbox-exporter /usr/local/bin/singbox-exporter
USER nobody:nogroup
EXPOSE 9098
HEALTHCHECK --interval=15s --timeout=5s --start-period=5s --retries=3 \
  CMD wget -q -O /dev/null http://127.0.0.1:9098/healthz || exit 1
ENTRYPOINT ["/usr/local/bin/singbox-exporter"]