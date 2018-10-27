FROM ubuntu
LABEL description="Resume from Rogier Lommers"
LABEL maintainer="Rogier Lommers <rogier@lommers.org>"

# add binary
COPY --chown=1000:1000 bin/dump-linux-amd64 /dump-linux-amd64
COPY --chown=1000:1000 /static /static

# change to data dir and run bianry
WORKDIR "/"
CMD ["/dump-linux-amd64", "-debug"]
