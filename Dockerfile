FROM alpine:3.24.3@sha256:be2656061b536c1883c4034790e21d0a09c5aaaf51968b89368a3c0371ea2c9a
LABEL description="Resume from Rogier Lommers"
LABEL maintainer="Rogier Lommers <rogier@lommers.org>"

# add binary and assets
COPY --chown=65532:65532 ./bin/resume /resume/resume
COPY --chown=65532:65532 ./src/assets /assets

# Uploaded build artifacts may not retain their executable mode.
RUN chmod 0555 /resume/resume

# binary will serve on 8080
EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD ["wget", "-q", "-O", "/dev/null", "http://127.0.0.1:8080/healthz"]

# run binary
USER 65532:65532
CMD ["/resume/resume"]
