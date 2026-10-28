FROM nginx@sha256:65645c7bb6a0661892a8b03b89d0743208a18dd2f3f17a54ef4b76fb8e2f2a10 AS certs

FROM scratch
COPY --from=certs /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY backend/bin/campusclaw-linux /usr/local/bin/campusclaw
EXPOSE 8080
HEALTHCHECK --interval=10s --timeout=5s --retries=6 --start-period=20s CMD ["/usr/local/bin/campusclaw", "--healthcheck"]
ENTRYPOINT ["/usr/local/bin/campusclaw"]
