; TorchLink camera live module - ZLMediaKit configuration template.
; Rendered by entrypoint.sh from IOT_VIDEO_* environment variables at start.
; Keys follow the pinned ZLMediaKit revision 10268396 (see docs/VIDEO_LIVE.md).

[api]
apiDebug=0
secret=__IOT_VIDEO_MEDIA_SECRET__
snapRoot=./www/snap/
defaultSnap=./www/logo.png
downloadRoot=/nonexistent

[general]
mediaServerId=__IOT_VIDEO_MEDIA_SERVER_ID__
enableVhost=0
flowThreshold=1024
maxStreamWaitMS=5000
; The platform decides when to release a stream; this only delays the hint.
streamNoneReaderDelayMS=20000
resetWhenRePlay=1
wait_track_ready_ms=10000
wait_audio_track_data_ms=1000
wait_add_track_ms=3000
unready_frame_cache=100
listen_ip=::

[hook]
enable=1
on_play=__IOT_VIDEO_HOOK_BASE__/on_play?secret=__IOT_VIDEO_HOOK_SECRET__
on_publish=__IOT_VIDEO_HOOK_BASE__/on_publish?secret=__IOT_VIDEO_HOOK_SECRET__
on_stream_none_reader=__IOT_VIDEO_HOOK_BASE__/on_stream_none_reader?secret=__IOT_VIDEO_HOOK_SECRET__
on_stream_not_found=__IOT_VIDEO_HOOK_BASE__/on_stream_not_found?secret=__IOT_VIDEO_HOOK_SECRET__
on_server_started=__IOT_VIDEO_HOOK_BASE__/on_server_started?secret=__IOT_VIDEO_HOOK_SECRET__
on_server_keepalive=__IOT_VIDEO_HOOK_BASE__/on_server_keepalive?secret=__IOT_VIDEO_HOOK_SECRET__
on_stream_changed=
on_flow_report=
on_http_access=
on_rtsp_auth=
on_rtsp_realm=
on_record_mp4=
on_record_ts=
on_shell_login=
on_server_exited=
on_send_rtp_stopped=
on_rtp_server_timeout=
timeoutSec=5
alive_interval=10.0
retry=1
retry_delay=2.0

[protocol]
modify_stamp=2
enable_audio=1
add_mute_audio=0
auto_close=0
continue_push_ms=3000
enable_hls=1
enable_hls_fmp4=0
enable_mp4=0
enable_rtsp=1
enable_rtmp=0
enable_ts=0
enable_fmp4=0
hls_save_path=/opt/media/hls
hls_demand=0
rtsp_demand=0

[hls]
fileBufSize=65536
segDur=2
segNum=3
segDelay=0
segRetain=5
broadcastRecordTs=0
deleteDelaySec=10
segKeep=0

[http]
charSet=utf-8
keepAliveSecond=30
maxReqSize=40960
port=80
sslport=0
rootPath=./www
dirMenu=0
allow_cross_domains=0
allow_ip_range=127.0.0.1,::1,10.0.0.0-10.255.255.255,172.16.0.0-172.31.255.255,192.168.0.0-192.168.255.255

[rtc]
; WebRTC media (ICE) port, UDP and TCP. Must be published with the same number
; and reachable from browsers at externIP. HTTPS proxying does not carry it.
port=__IOT_VIDEO_RTC_PORT__
tcpPort=__IOT_VIDEO_RTC_PORT__
externIP=__IOT_VIDEO_RTC_EXTERN_IP__
signalingPort=0
signalingSslPort=0
icePort=0
iceTcpPort=0
enableTurn=0
timeoutSec=15
preferredCodecA=PCMA,PCMU,opus
preferredCodecV=H264,H265

[rtsp]
; Internal only (FFmpeg reads and publishes over loopback); never published.
port=554
sslport=0
authBasic=0
; 0 = re-packetize pulled RTSP (no re-encoding) so SPS/PPS precede every IDR.
; With 1 the camera's RTP is forwarded as-is; cameras that send parameter sets
; only in the SDP then freeze in Chrome's WebRTC after the first GOP.
directProxy=0
handshakeSecond=15
keepAliveSecond=15
lowLatency=0

[rtmp]
port=0
sslport=0

[srt]
port=0

[rtp_proxy]
port=0

[shell]
port=0

[onvif]
port=0

[ffmpeg]
bin=/usr/bin/ffmpeg
log=./ffmpeg/ffmpeg.log
restart_sec=0
; Default command kept for ZLMediaKit compatibility; the platform never uses it
; and verifies the running command's encoder for every transcode it starts.
cmd=%s -re -i %s -c:a aac -strict -2 -ar 44100 -ab 48k -c:v libx264 -f flv %s
snap=%s -i %s -y -f mjpeg -frames:v 1 -an %s
; Controlled output templates: %s = ffmpeg binary, source URL, target URL.
; Video: H.264 without B-frames (WebRTC safe), capped size and frame rate, one
; slice per frame (zerolatency would otherwise enable sliced threads).
; Audio (when present): AAC mono 64 kbps.
iot_h264_1080p=%s -hide_banner -loglevel warning -rtsp_transport tcp -i %s -map 0:v:0 -map 0:a:0? -c:v libx264 -preset veryfast -tune zerolatency -x264-params sliced-threads=0 -profile:v main -bf 0 -pix_fmt yuv420p -vf scale=w=1920:h=1080:force_original_aspect_ratio=decrease:force_divisible_by=2,fps=25 -g 50 -keyint_min 50 -sc_threshold 0 -b:v 3000k -maxrate 3500k -bufsize 6000k -threads __IOT_VIDEO_TRANSCODE_THREADS__ -c:a aac -ar 44100 -ac 1 -b:a 64k -f rtsp -rtsp_transport tcp %s
iot_h264_720p=%s -hide_banner -loglevel warning -rtsp_transport tcp -i %s -map 0:v:0 -map 0:a:0? -c:v libx264 -preset veryfast -tune zerolatency -x264-params sliced-threads=0 -profile:v main -bf 0 -pix_fmt yuv420p -vf scale=w=1280:h=720:force_original_aspect_ratio=decrease:force_divisible_by=2,fps=25 -g 50 -keyint_min 50 -sc_threshold 0 -b:v 1500k -maxrate 1800k -bufsize 3000k -threads __IOT_VIDEO_TRANSCODE_THREADS__ -c:a aac -ar 44100 -ac 1 -b:a 64k -f rtsp -rtsp_transport tcp %s
iot_h264_480p=%s -hide_banner -loglevel warning -rtsp_transport tcp -i %s -map 0:v:0 -map 0:a:0? -c:v libx264 -preset veryfast -tune zerolatency -x264-params sliced-threads=0 -profile:v main -bf 0 -pix_fmt yuv420p -vf scale=w=854:h=480:force_original_aspect_ratio=decrease:force_divisible_by=2,fps=15 -g 30 -keyint_min 30 -sc_threshold 0 -b:v 800k -maxrate 1000k -bufsize 1600k -threads __IOT_VIDEO_TRANSCODE_THREADS__ -c:a aac -ar 44100 -ac 1 -b:a 64k -f rtsp -rtsp_transport tcp %s
iot_audio_aac=%s -hide_banner -loglevel warning -rtsp_transport tcp -i %s -map 0:v:0 -map 0:a:0? -c:v copy -c:a aac -ar 44100 -ac 1 -b:a 64k -f rtsp -rtsp_transport tcp %s
