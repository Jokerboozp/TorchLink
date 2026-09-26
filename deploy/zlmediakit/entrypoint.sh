#!/bin/sh
# Render the ZLMediaKit configuration from environment variables, then start
# the media server. Secrets are validated to a safe character set so they can
# be substituted without escaping; the rendered file stays inside the container.
set -eu

template=/opt/torchlink/config.ini.tpl
target=/opt/media/conf/config.ini

require() {
  name="$1"; value="$2"; pattern="$3"
  if [ -z "$value" ]; then echo "missing $name" >&2; exit 64; fi
  if ! printf '%s' "$value" | grep -Eq "^${pattern}\$"; then echo "invalid $name" >&2; exit 64; fi
}

require IOT_VIDEO_MEDIA_SECRET "${IOT_VIDEO_MEDIA_SECRET:-}" '[A-Za-z0-9._-]{24,128}'
require IOT_VIDEO_HOOK_SECRET "${IOT_VIDEO_HOOK_SECRET:-}" '[A-Za-z0-9._-]{24,128}'
require IOT_VIDEO_MEDIA_SERVER_ID "${IOT_VIDEO_MEDIA_SERVER_ID:-torchlink-media-1}" '[A-Za-z0-9._-]{1,64}'
require IOT_VIDEO_HOOK_BASE "${IOT_VIDEO_HOOK_BASE:-}" 'https?://[A-Za-z0-9._:-]+(/[A-Za-z0-9._/-]*)?'
require IOT_VIDEO_RTC_PORT "${IOT_VIDEO_RTC_PORT:-8000}" '[0-9]{2,5}'
require IOT_VIDEO_TRANSCODE_THREADS "${IOT_VIDEO_TRANSCODE_THREADS:-2}" '[1-9][0-9]?'
extern_ip="${IOT_VIDEO_RTC_EXTERN_IP:-}"
if [ -n "$extern_ip" ]; then require IOT_VIDEO_RTC_EXTERN_IP "$extern_ip" '[0-9A-Fa-f.:,]{2,200}'; fi

sed \
  -e "s|__IOT_VIDEO_MEDIA_SECRET__|${IOT_VIDEO_MEDIA_SECRET}|g" \
  -e "s|__IOT_VIDEO_HOOK_SECRET__|${IOT_VIDEO_HOOK_SECRET}|g" \
  -e "s|__IOT_VIDEO_MEDIA_SERVER_ID__|${IOT_VIDEO_MEDIA_SERVER_ID:-torchlink-media-1}|g" \
  -e "s|__IOT_VIDEO_HOOK_BASE__|${IOT_VIDEO_HOOK_BASE%/}|g" \
  -e "s|__IOT_VIDEO_RTC_PORT__|${IOT_VIDEO_RTC_PORT:-8000}|g" \
  -e "s|__IOT_VIDEO_RTC_EXTERN_IP__|${extern_ip}|g" \
  -e "s|__IOT_VIDEO_TRANSCODE_THREADS__|${IOT_VIDEO_TRANSCODE_THREADS:-2}|g" \
  "$template" > "$target.tmp"
chmod 600 "$target.tmp"
mv "$target.tmp" "$target"
mkdir -p /opt/media/hls /opt/media/bin/ffmpeg

cd /opt/media/bin
# Log level 3 (warning) keeps pull URLs and player details out of routine logs.
exec ./MediaServer -s default.pem -c ../conf/config.ini -l "${IOT_VIDEO_MEDIA_LOG_LEVEL:-3}"
