package model

import (
	"errors"
	"fmt"
)

// AlarmAttachment is a photo, video or document added while handling an
// alarm. The file is kept in object storage under AlarmAttachmentKey.
type AlarmAttachment struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	ContentType string `json:"contentType"`
	Size        int64  `json:"size"`
	SHA256      string `json:"sha256"`
	UploadedBy  string `json:"uploadedBy"`
	UploadedAt  int64  `json:"uploadedAt"`
}

const (
	AlarmAttachmentBucket = "iot-alarm-attachments"
	// MaxAlarmAttachments bounds the attachments of one alarm.
	MaxAlarmAttachments = 10
	// MaxAlarmAttachmentSize bounds one attachment.
	MaxAlarmAttachmentSize = 20 << 20
)

var (
	ErrAlarmClosed        error = Invalid("已关闭的告警不能再修改附件")
	ErrTooManyAttachments error = Invalid(fmt.Sprintf("每条告警最多 %d 个附件", MaxAlarmAttachments))
	ErrAttachmentNotFound       = errors.New("附件不存在")
)

// AlarmAttachmentKey is the object key of an attachment.
func AlarmAttachmentKey(tenant, alarmID, attachmentID string) string {
	return tenant + "/alarms/" + alarmID + "/" + attachmentID
}

// ObjectRef names a file in object storage.
type ObjectRef struct {
	Bucket string `json:"bucket"`
	Key    string `json:"key"`
}
