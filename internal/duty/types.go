// Package duty owns the tenant-scoped duty roster, actual attendance and
// immutable handover workflow. Model generation is an optional adapter.
package duty

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"iot-platform/internal/model"
)

var (
	ErrForbidden = errors.New("无权执行值班操作")
	ErrConflict  = errors.New("值班数据已变化，请刷新后重新确认")
	ErrNotFound  = errors.New("值班记录不存在")
)

type ValidationError struct {
	Message string `json:"message"`
}

func (e *ValidationError) Error() string       { return e.Message }
func invalid(format string, args ...any) error { return &ValidationError{fmt.Sprintf(format, args...)} }

type Actor struct {
	TenantID         string   `json:"tenantId"`
	Username         string   `json:"username"`
	Admin            bool     `json:"admin"`
	Enabled          bool     `json:"enabled"`
	Permissions      []string `json:"permissions"`
	AllowedDeviceIDs []string `json:"allowedDeviceIds"`
	AllDevices       bool     `json:"allDevices"`
	SessionVersion   int64    `json:"sessionVersion"`
	AccessVersion    string   `json:"accessVersion"`
	ManagedUser      bool     `json:"managedUser"`
}
type ResolveActor func(context.Context, string, string) (Actor, error)

type Command struct {
	Kind            string          `json:"kind"`
	ID              string          `json:"id"`
	Operation       string          `json:"operation"`
	ExpectedVersion int64           `json:"expectedVersion"`
	IdempotencyKey  string          `json:"idempotencyKey"`
	Body            json.RawMessage `json:"body"`
}
type Result struct {
	Items []model.DutyDocument `json:"items"`
	Total int                  `json:"total"`
}

type Station = model.DutyStation
type Team = model.DutyTeam
type ShiftTemplate = model.DutyShiftTemplate
type Roster = model.DutyRoster
type Attendance = model.DutyAttendance
type Run = model.DutyRun
type Record = model.DutyRecord
type Item = model.DutyItem
type ItemEvent = model.DutyItemEvent
type Handover = model.DutyHandover
type Revision = model.DutyHandoverRevision
type AIJob = model.DutyAIJob
type Notice = model.DutyNotification
type ImportRow struct {
	StationID  string   `json:"stationId"`
	TemplateID string   `json:"templateId"`
	TeamID     string   `json:"teamId"`
	Date       string   `json:"date"`
	Start      int64    `json:"startAt"`
	End        int64    `json:"endAt"`
	Members    []string `json:"memberIds"`
	Leader     string   `json:"leaderId"`
	SourceID   string   `json:"sourceId"`
}
type ImportProblem struct {
	Row     int    `json:"row"`
	Message string `json:"message"`
}
type ImportPreview struct {
	Rows     []Roster        `json:"rows"`
	Problems []ImportProblem `json:"problems"`
	Digest   string          `json:"digest"`
}
type GenerateRequest struct {
	StationID  string   `json:"stationId"`
	TemplateID string   `json:"templateId"`
	TeamID     string   `json:"teamId"`
	From       string   `json:"from"`
	To         string   `json:"to"`
	Members    []string `json:"memberIds"`
	Leader     string   `json:"leaderId"`
}
type ImportRequest struct {
	CSV    string      `json:"csv"`
	Rows   []ImportRow `json:"rows"`
	Digest string      `json:"digest"`
}
