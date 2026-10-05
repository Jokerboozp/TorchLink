# syntax=docker/dockerfile:1.7
#
# Bundled knowledge models on the llama.cpp server, for linux/amd64 and
# linux/arm64: bge-m3 (embedding, 1024 dimensions) and bge-reranker-v2-m3
# (reranking), both Q8_0 GGUF. One image holds both models; Compose runs it
# twice (services embedding and reranker) with different LLAMA_ARG_* settings.
#
# Models are downloaded at build time from pinned revisions and verified by
# SHA-256, so running containers and offline packages never download. Set
# IOT_HF_ENDPOINT (for example https://hf-mirror.com) where huggingface.co is
# unreachable; the checksums still apply. IOT_LLAMA_CPP_IMAGE may point at a
# mirror registry holding the same multi-arch image (keep the digest).
ARG LLAMA_CPP_IMAGE=ghcr.io/ggml-org/llama.cpp:server-b11382@sha256:559ac229adefe0f7e2d4e32f5222f26927b6bb8ba44b9db08e2544afebf41984
FROM ${LLAMA_CPP_IMAGE}

ARG HF_ENDPOINT=https://huggingface.co

LABEL org.opencontainers.image.title="TorchLink local AI models" \
      org.opencontainers.image.description="llama.cpp b11382 with bge-m3 and bge-reranker-v2-m3 (Q8_0)"

RUN mkdir -p /models

RUN curl -fL --retry 5 --retry-delay 5 -o /models/bge-m3-Q8_0.gguf \
      "${HF_ENDPOINT%/}/gpustack/bge-m3-GGUF/resolve/2d48f1737679ad900d5c26c5aad5410e9c70fdca/bge-m3-Q8_0.gguf" \
    && echo '950f4a8e5e19477a6d3c26d2f162233c20002c601f75e4b002e3239997821167  /models/bge-m3-Q8_0.gguf' | sha256sum -c -

RUN curl -fL --retry 5 --retry-delay 5 -o /models/bge-reranker-v2-m3-Q8_0.gguf \
      "${HF_ENDPOINT%/}/gpustack/bge-reranker-v2-m3-GGUF/resolve/3093af03b1a635e67b084b1d8c03c5f5e020fd05/bge-reranker-v2-m3-Q8_0.gguf" \
    && echo 'a43c7c9b11a4c1517e5bf95151960e1621d1b72f7a493364b01e386cf1aaa1d3  /models/bge-reranker-v2-m3-Q8_0.gguf' | sha256sum -c -

# Shared server settings; the model and its mode come from Compose.
ENV LLAMA_ARG_HOST=0.0.0.0 \
    LLAMA_ARG_PORT=8080 \
    LLAMA_ARG_CTX_SIZE=8192 \
    LLAMA_ARG_BATCH=8192 \
    LLAMA_ARG_UBATCH=8192
EXPOSE 8080
