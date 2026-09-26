# Camera live media server: the official ZLMediaKit open-source image pinned by
# digest (master @ revision 10268396b89748bc2b0d19dda79bc29219e97815, built
# 2026-09-22), plus the TorchLink configuration template. The base image ships
# FFmpeg 6.1.1 (Ubuntu 24.04 build with libx264/libx265/libopus), which the
# optional transcode profiles use through ZLMediaKit's addFFmpegSource API.
FROM zlmediakit/zlmediakit:master@sha256:48d1b3c264dd6537f9dcec12fb16b541ed6c9dc5a54322c328c3135fec5c71c8
COPY config.ini.tpl /opt/torchlink/config.ini.tpl
COPY entrypoint.sh /opt/torchlink/entrypoint.sh
RUN chmod 0755 /opt/torchlink/entrypoint.sh
ENTRYPOINT ["/opt/torchlink/entrypoint.sh"]
