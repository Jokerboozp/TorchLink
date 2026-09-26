package video

import (
	"errors"
	"fmt"
)

// Output profiles are fixed server-side. Browsers and administrators choose a
// profile ID only; FFmpeg arguments live in the media server's config.ini
// ([ffmpeg] iot_* templates) and are never built from user input.
//
// Protocol conversion (RTSP → WebRTC/HLS) is done by the media server without
// changing codecs. Transcoding (re-encoding video and/or audio) only happens
// for the profiles below and only when the transcode switch is on.
type outputProfile struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	CmdKey      string `json:"-"`
	// Encoder is the codec option asserted against the command the media
	// server actually runs (as a whole argument pair, not a substring),
	// so a missing template (which would fall back to the default command)
	// is detected instead of silently producing different output.
	Encoder   string `json:"-"`
	Transcode bool   `json:"transcode"`
}

const (
	profileDirect  = "direct"
	defaultProfile = "h264_720p"
)

var outputProfiles = []outputProfile{
	{ID: profileDirect, Name: "直接播放（不转码）", Description: "转协议播放摄像头原始码流，不改变音视频编码。"},
	{ID: "h264_1080p", Name: "H.264 1080p", Description: "视频转为 H.264（无 B 帧），最高 1920×1080、25 fps、约 3 Mbps；音频转 AAC。", CmdKey: "ffmpeg.iot_h264_1080p", Encoder: "-c:v libx264", Transcode: true},
	{ID: "h264_720p", Name: "H.264 720p", Description: "视频转为 H.264（无 B 帧），最高 1280×720、25 fps、约 1.5 Mbps；音频转 AAC。", CmdKey: "ffmpeg.iot_h264_720p", Encoder: "-c:v libx264", Transcode: true},
	{ID: "h264_480p", Name: "H.264 480p", Description: "视频转为 H.264（无 B 帧），最高 854×480、15 fps、约 0.8 Mbps；音频转 AAC。", CmdKey: "ffmpeg.iot_h264_480p", Encoder: "-c:v libx264", Transcode: true},
	{ID: "audio_aac", Name: "视频直通 + 音频转 AAC", Description: "视频不重新编码，仅把音频转为 AAC，用于 HLS 播放 G.711 等音频。", CmdKey: "ffmpeg.iot_audio_aac", Encoder: "-c:a aac", Transcode: true},
}

func findProfile(id string) (outputProfile, bool) {
	for _, p := range outputProfiles {
		if p.ID == id {
			return p, true
		}
	}
	return outputProfile{}, false
}

// OutputProfiles lists profiles for the UI.
func OutputProfiles() []outputProfile { return append([]outputProfile(nil), outputProfiles...) }

// Transcode modes of a camera.
const (
	transcodeOff   = "off"
	transcodeAuto  = "auto"
	transcodeFixed = "fixed"
)

// clientCaps is what the player reports about the browser.
type clientCaps struct {
	WebRTCH265 bool `json:"webrtcH265"`
	HLSH265    bool `json:"hlsH265"`
}

var errNeedsTranscode = errors.New("needs transcode")

// chooseProfile decides the output profile for one viewer. Only video codec
// compatibility triggers automatic transcoding: playback starts muted, so
// audio is passed through and never causes an automatic transcode.
//
//   - H.265 is direct only when the browser reports H.265 support for the
//     chosen protocol; otherwise it needs transcoding.
//   - H.264 with B-frames (declared by the administrator, since SDP does not
//     reveal it) breaks WebRTC ordering and needs transcoding there.
//   - Unknown or other codecs are not claimed to be playable.
func chooseProfile(mode, fixed, videoCodec string, bFrames bool, protocol string, caps clientCaps, transcodeAvailable bool) (string, string, error) {
	if mode == transcodeFixed {
		if !transcodeAvailable {
			return "", "", errors.New("已指定兼容输出，但转码未启用")
		}
		if _, ok := findProfile(fixed); !ok || fixed == profileDirect {
			return "", "", errors.New("指定的输出规格无效")
		}
		return fixed, "按配置使用指定兼容输出", nil
	}
	reason := ""
	switch videoCodec {
	case "H264":
		if bFrames && protocol == "webrtc" {
			reason = "源 H.264 含 B 帧，WebRTC 播放需要转码"
		}
	case "H265":
		if (protocol == "webrtc" && !caps.WebRTCH265) || (protocol == "hls" && !caps.HLSH265) {
			reason = fmt.Sprintf("当前浏览器不支持通过 %s 播放 H.265", map[string]string{"webrtc": "WebRTC", "hls": "HLS"}[protocol])
		}
	case "":
		return "", "", errors.New("尚未获取到源视频编码")
	default:
		reason = "源视频编码 " + videoCodec + " 浏览器无法直接播放"
	}
	if reason == "" {
		return profileDirect, "", nil
	}
	if mode != transcodeAuto || !transcodeAvailable {
		return "", reason, errNeedsTranscode
	}
	target := fixed
	if p, ok := findProfile(target); !ok || !p.Transcode || p.ID == "audio_aac" {
		target = defaultProfile
	}
	return target, reason + "，已自动转码", nil
}

// audioNote explains what will happen with the source audio per protocol.
func audioNote(audioCodec, profile, protocol string) string {
	if audioCodec == "" {
		return "源码流无音频"
	}
	if p, ok := findProfile(profile); ok && p.Transcode {
		if protocol == "hls" {
			return "音频已转为 AAC，默认静音，可手动开启"
		}
		return "WebRTC 不支持 AAC 音频，本输出仅播放画面"
	}
	switch protocol {
	case "webrtc":
		if audioCodec == "PCMA" || audioCodec == "PCMU" || audioCodec == "OPUS" {
			return "默认静音，可手动开启声音"
		}
		return "WebRTC 不支持源音频编码 " + audioCodec + "，仅播放画面"
	default:
		if audioCodec == "AAC" {
			return "默认静音，可手动开启声音"
		}
		return "HLS 不支持源音频编码 " + audioCodec + "，仅播放画面；可由管理员选择“视频直通 + 音频转 AAC”"
	}
}
