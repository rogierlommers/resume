FROM ubuntu
LABEL description="Resume from Rogier Lommers"
LABEL maintainer="Rogier Lommers <rogier@lommers.org>"

# add binary and assets
COPY --chown=1000:1000 bin/resume /resume/
COPY --chown=1000:1000 /src/static /resume

# run binary
WORKDIR "/resume"
CMD ["resume"]
