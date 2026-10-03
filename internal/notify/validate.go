package notify

import (
	"errors"
	"regexp"
	"strings"
)

var (
	idPattern     = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,64}$`)
	emailPattern  = regexp.MustCompile(`^[^@\s]{1,64}@[^@\s]{1,190}\.[^@\s]{2,}$`)
	mobilePattern = regexp.MustCompile(`^\+?[0-9][0-9-]{5,19}$`)
)

const (
	maxStages     = 8
	maxStageDelay = 24 * 60 * 60
	maxListItems  = 100
)

// ValidChannelType reports whether the type is supported.
func ValidChannelType(kind string) bool {
	switch kind {
	case ChannelSMTP, ChannelWeCom, ChannelDingTalk, ChannelFeishu, ChannelWebhook:
		return true
	}
	return false
}

// ValidatePolicy checks a policy against the tenant's channels.
func ValidatePolicy(p Policy, channels map[string]Channel) error {
	if !idPattern.MatchString(p.ID) {
		return errors.New("策略标识须为 1 至 64 位字母、数字、点、横线或下划线")
	}
	if name := strings.TrimSpace(p.Name); name == "" || len([]rune(name)) > 64 {
		return errors.New("请填写不超过 64 个字的策略名称")
	}
	if len(p.Stages) == 0 || len(p.Stages) > maxStages {
		return errors.New("通知策略须包含 1 至 8 级通知")
	}
	if len(p.Levels) > maxListItems || len(p.AlarmTypes) > maxListItems || len(p.ProductIDs) > maxListItems {
		return errors.New("筛选条件过多")
	}
	previous := -1
	for i, stage := range p.Stages {
		if i == 0 && stage.DelaySeconds != 0 {
			return errors.New("第 1 级通知须立即发送")
		}
		if stage.DelaySeconds <= previous || stage.DelaySeconds > maxStageDelay {
			return errors.New("升级等待时间须逐级递增且不超过 24 小时")
		}
		previous = stage.DelaySeconds
		if len(stage.ChannelIDs) == 0 {
			return errors.New("每级通知至少选择一个通知渠道")
		}
		for _, id := range stage.ChannelIDs {
			if _, ok := channels[id]; !ok {
				return errors.New("通知渠道不存在")
			}
		}
		if len(stage.Users)+len(stage.Roles)+len(stage.Emails)+len(stage.Mobiles)+len(stage.StationIDs) > maxListItems {
			return errors.New("接收人过多")
		}
		for _, v := range stage.Emails {
			if !emailPattern.MatchString(v) {
				return errors.New("邮箱格式无效：" + v)
			}
		}
		for _, v := range stage.Mobiles {
			if !mobilePattern.MatchString(v) {
				return errors.New("手机号格式无效：" + v)
			}
		}
	}
	return nil
}

// ValidateChannel checks the channel fields saved by administrators.
func ValidateChannel(c Channel) error {
	if !idPattern.MatchString(c.ID) {
		return errors.New("渠道标识须为 1 至 64 位字母、数字、点、横线或下划线")
	}
	if name := strings.TrimSpace(c.Name); name == "" || len([]rune(name)) > 64 {
		return errors.New("请填写不超过 64 个字的渠道名称")
	}
	if !ValidChannelType(c.Type) {
		return errors.New("不支持的通知渠道类型")
	}
	if c.Type == ChannelSMTP && !emailPattern.MatchString(c.Config.From) {
		return errors.New("发件人邮箱格式无效")
	}
	return nil
}

// ReferencedBy lists the policies that use a channel.
func ReferencedBy(policies []Policy, channelID string) []string {
	out := []string{}
	for _, p := range policies {
		for _, stage := range p.Stages {
			if containsString(stage.ChannelIDs, channelID) {
				out = append(out, p.Name)
				break
			}
		}
	}
	return out
}

func containsString(list []string, v string) bool {
	for _, item := range list {
		if item == v {
			return true
		}
	}
	return false
}
