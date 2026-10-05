FROM postgres:17-alpine3.22 AS vector-build
# Keep PostgreSQL's existing major version and on-disk layout. Build portable
# code for amd64/arm64 rather than binding the extension to the build CPU.
# Override ALPINE_MIRROR (IOT_ALPINE_MIRROR) where dl-cdn.alpinelinux.org is
# unreachable, for example https://mirrors.aliyun.com/alpine.
ARG ALPINE_MIRROR=https://dl-cdn.alpinelinux.org/alpine
RUN sed -i "s#https\?://dl-cdn.alpinelinux.org/alpine#${ALPINE_MIRROR%/}#" /etc/apk/repositories \
    && apk add --no-cache build-base clang19 llvm19-dev curl
WORKDIR /tmp
RUN curl -fSL --retry 3 -o pgvector.tar.gz https://codeload.github.com/pgvector/pgvector/tar.gz/refs/tags/v0.8.1 \
    && echo 'a9094dfb85ccdde3cbb295f1086d4c71a20db1d26bf1d6c39f07a7d164033eb4  pgvector.tar.gz' | sha256sum -c - \
    && tar -xzf pgvector.tar.gz \
    && cd pgvector-0.8.1 \
    && make OPTFLAGS='' \
    && make install DESTDIR=/out

FROM postgres:17-alpine3.22
COPY --from=vector-build /out/ /
LABEL org.opencontainers.image.title="TorchLink PostgreSQL with pgvector" \
      org.opencontainers.image.version="17-pgvector-0.8.1" \
      org.opencontainers.image.source="https://github.com/pgvector/pgvector/tree/v0.8.1"
