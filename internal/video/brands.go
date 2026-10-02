package video

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
)

// Brand templates only help an administrator compose a common RTSP path. A
// template is not a compatibility guarantee: models and firmware differ, and
// every configuration must pass a connection test.
type brandTemplate struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Port  int    `json:"defaultPort"`
	Hint  string `json:"hint"`
	build func(channel int, sub bool) string
}

var brandTemplates = []brandTemplate{
	{ID: "hikvision", Name: "海康威视", Port: 554, Hint: "路径 /Streaming/Channels/{通道}01（主码流）或 {通道}02（子码流）；NVR 通道号从 1 开始。", build: func(ch int, sub bool) string {
		return fmt.Sprintf("/Streaming/Channels/%d%02d", ch, map[bool]int{false: 1, true: 2}[sub])
	}},
	{ID: "dahua", Name: "大华", Port: 554, Hint: "路径 /cam/realmonitor?channel={通道}&subtype=0（主码流）或 1（子码流）。", build: func(ch int, sub bool) string {
		return fmt.Sprintf("/cam/realmonitor?channel=%d&subtype=%d", ch, map[bool]int{false: 0, true: 1}[sub])
	}},
	{ID: "uniview", Name: "宇视", Port: 554, Hint: "路径 /unicast/c{通道}/s0/live（主码流）或 s1（子码流）；部分老型号使用 /media/video1。", build: func(ch int, sub bool) string {
		return fmt.Sprintf("/unicast/c%d/s%d/live", ch, map[bool]int{false: 0, true: 1}[sub])
	}},
	{ID: "generic", Name: "通用 RTSP", Port: 554, Hint: "手动填写不含账号密码的 rtsp:// 地址。"},
}

func findBrand(id string) (brandTemplate, bool) {
	for _, b := range brandTemplates {
		if b.ID == id {
			return b, true
		}
	}
	return brandTemplate{}, false
}

// BrandTemplates lists the templates for the UI.
func BrandTemplates() []map[string]any {
	out := make([]map[string]any, 0, len(brandTemplates))
	for _, b := range brandTemplates {
		out = append(out, map[string]any{"id": b.ID, "name": b.Name, "defaultPort": b.Port, "hint": b.Hint, "structured": b.build != nil})
	}
	return out
}

// buildTemplateURL composes a credential-free RTSP URL for a brand template.
func buildTemplateURL(brand, host string, port, channel int, sub bool) (string, error) {
	b, ok := findBrand(brand)
	if !ok || b.build == nil {
		return "", errors.New("所选品牌没有结构化模板，请手动填写流地址")
	}
	host = strings.TrimSpace(host)
	if host == "" || strings.ContainsAny(host, "/\\@?#% ") {
		return "", errors.New("摄像头地址无效")
	}
	if port == 0 {
		port = b.Port
	}
	if port < 1 || port > 65535 {
		return "", errors.New("RTSP 端口无效")
	}
	if channel < 1 || channel > 512 {
		return "", errors.New("通道号须在 1 到 512 之间")
	}
	path := b.build(channel, sub)
	u := &url.URL{Scheme: "rtsp", Host: net.JoinHostPort(strings.Trim(host, "[]"), strconv.Itoa(port))}
	if i := strings.IndexByte(path, '?'); i >= 0 {
		u.Path, u.RawQuery = path[:i], path[i+1:]
	} else {
		u.Path = path
	}
	return u.String(), nil
}
