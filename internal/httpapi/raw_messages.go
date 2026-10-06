package httpapi

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"iot-platform/internal/devicescope"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"iot-platform/internal/core"
	"iot-platform/internal/model"
)

func (s *Server) listRaw(w http.ResponseWriter, r *http.Request) {
	c := claims(r)
	q := r.URL.Query()
	pagination := parseListPagination(r)
	filter, filterErr := parseRawFilter(q)
	if filterErr != nil {
		problem(w, http.StatusUnprocessableEntity, filterErr.Error())
		return
	}
	filter.TenantID, filter.Limit, filter.Offset = c.TenantID, pagination.PageSize, pagination.Offset
	extra := map[string]any{}
	// An unfiltered listing reads only the recent partitions; device, message
	// and time filters keep their full range.
	if filter.Start == 0 && filter.End == 0 && filter.MessageID == "" && filter.DeviceID == "" {
		filter.Start = time.Now().Add(-rawDefaultWindow).UnixMilli()
		extra["window"] = map[string]any{"start": filter.Start, "defaulted": true}
	}
	// Counting stops past rawCountCap; the page shows "10000+".
	filter.CountLimit = rawCountCap + 1
	items, err := s.engine.Repo.ListRawIndexes(r.Context(), filter)
	if err != nil {
		s.fail(w, r, err, "")
		return
	}
	total, err := s.engine.Repo.CountRawIndexes(r.Context(), filter)
	if err != nil {
		s.fail(w, r, err, "")
		return
	}
	ids := make([]string, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.MessageID)
	}
	messages, err := s.engine.Repo.GetStandardMessagesByRawIDs(r.Context(), c.TenantID, ids)
	if err != nil {
		messages = map[string]model.StandardMessage{}
		for _, id := range ids {
			if message, getErr := s.engine.Repo.GetStandardMessageByRaw(r.Context(), c.TenantID, id); getErr == nil {
				messages[id] = message
			}
		}
	}
	for i := range items {
		if message, ok := messages[items[i].MessageID]; ok {
			items[i].Parsed = true
			items[i].ParsedMessageType = string(message.MessageType)
			items[i].Parser = message.Parser
		}
	}
	extra["totalCapped"] = total > rawCountCap
	writeList(w, 200, items, total, pagination, extra)
}

const (
	rawCountCap      = 10000
	rawDefaultWindow = 7 * 24 * time.Hour
)

func (s *Server) rawDetail(w http.ResponseWriter, r *http.Request) {
	idx, err := s.engine.Repo.GetRawIndex(r.Context(), claims(r).TenantID, r.PathValue("id"))
	if err != nil {
		problem(w, 404, "raw message not found")
		return
	}
	raw, err := s.engine.GetRaw(r.Context(), idx)
	if err != nil {
		s.fail(w, r, err, "raw archive could not be read")
		return
	}
	result := map[string]any{"archive": idx, "message": raw, "parseStatus": "UNPARSED", "parseError": idx.ParseError}
	if idx.ParseError != "" {
		result["parseStatus"] = "FAILED"
	}
	if standard, parseErr := s.engine.Repo.GetStandardMessageByRaw(r.Context(), claims(r).TenantID, idx.MessageID); parseErr == nil {
		result["parseStatus"] = "PARSED"
		result["standardMessage"] = standard
	}
	write(w, 200, result)
}

func (s *Server) downloadRaw(w http.ResponseWriter, r *http.Request) {
	idx, err := s.engine.Repo.GetRawIndex(r.Context(), claims(r).TenantID, r.PathValue("id"))
	if err != nil {
		problem(w, 404, "raw message not found")
		return
	}
	raw, err := s.engine.GetRaw(r.Context(), idx)
	if err != nil {
		s.fail(w, r, err, "raw archive could not be read")
		return
	}
	body, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		s.fail(w, r, err, "")
		return
	}
	filename := strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.' {
			return r
		}
		return '_'
	}, idx.MessageID) + ".json"
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	w.Header().Set("X-Content-SHA256", idx.PayloadHash)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(append(body, '\n'))
	s.audit(r, "raw.download", "raw-message", idx.MessageID, map[string]any{"payloadHash": idx.PayloadHash})
}

func (s *Server) downloadRawBatch(w http.ResponseWriter, r *http.Request) {
	var in struct {
		MessageIDs []string `json:"messageIds"`
	}
	if decode(w, r, &in) != nil {
		return
	}
	if len(in.MessageIDs) == 0 {
		problem(w, http.StatusUnprocessableEntity, "at least one messageId is required")
		return
	}
	if len(in.MessageIDs) > 500 {
		problem(w, 422, "no more than 500 raw messages may be downloaded at once")
		return
	}
	type archivedRaw struct {
		Index   model.RawArchiveIndex
		Message model.RawMessage
	}
	items := make([]archivedRaw, 0, len(in.MessageIDs))
	seen := make(map[string]struct{}, len(in.MessageIDs))
	totalPayloadSize := 0
	for _, id := range in.MessageIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		idx, err := s.engine.Repo.GetRawIndex(r.Context(), claims(r).TenantID, id)
		if err != nil {
			problem(w, 404, "raw message not found: "+id)
			return
		}
		totalPayloadSize += idx.PayloadSize
		if totalPayloadSize > 100*1024*1024 {
			problem(w, 413, "selected raw messages exceed the 100 MiB batch limit")
			return
		}
		raw, err := s.engine.GetRaw(r.Context(), idx)
		if err != nil {
			s.fail(w, r, err, "raw archive could not be read: "+id)
			return
		}
		items = append(items, archivedRaw{Index: idx, Message: raw})
	}
	if len(items) == 0 {
		problem(w, http.StatusUnprocessableEntity, "at least one valid messageId is required")
		return
	}
	var archive bytes.Buffer
	zw := zip.NewWriter(&archive)
	manifest := make([]model.RawArchiveIndex, 0, len(items))
	for i, item := range items {
		body, err := json.MarshalIndent(item.Message, "", "  ")
		if err != nil {
			s.fail(w, r, err, "")
			return
		}
		name := fmt.Sprintf("报文/%03d_%s.json", i+1, safeAttachmentName(item.Index.MessageID))
		file, err := zw.Create(name)
		if err != nil {
			s.fail(w, r, err, "")
			return
		}
		if _, err = file.Write(append(body, '\n')); err != nil {
			s.fail(w, r, err, "")
			return
		}
		manifest = append(manifest, item.Index)
	}
	manifestBody, _ := json.MarshalIndent(map[string]any{"exportedAt": time.Now().UnixMilli(), "count": len(items), "items": manifest}, "", "  ")
	manifestFile, err := zw.Create("清单.json")
	if err == nil {
		_, err = manifestFile.Write(append(manifestBody, '\n'))
	}
	if err != nil {
		s.fail(w, r, err, "")
		return
	}
	if err = zw.Close(); err != nil {
		s.fail(w, r, err, "")
		return
	}
	filename := fmt.Sprintf("原始报文_%s_%d条.zip", time.Now().Format("20060102_150405"), len(items))
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="raw-messages.zip"; filename*=UTF-8''%s`, url.QueryEscape(filename)))
	w.Header().Set("X-Archive-Count", strconv.Itoa(len(items)))
	w.Header().Set("Content-Length", strconv.Itoa(archive.Len()))
	w.WriteHeader(http.StatusOK)
	_, _ = archive.WriteTo(w)
	s.audit(r, "raw.download.batch", "raw-message", "batch", map[string]any{"count": len(items), "payloadBytes": totalPayloadSize})
}

func safeAttachmentName(value string) string {
	return strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.' {
			return r
		}
		return '_'
	}, value)
}

func (s *Server) startReplay(w http.ResponseWriter, r *http.Request) {
	var v model.ReplayRequest
	if decode(w, r, &v) != nil {
		return
	}
	c := claims(r)
	v.TenantID = c.TenantID
	v.CreatedBy = c.Username
	v.CapacityRunID = capacityRequestRunID(r)
	task, err := s.engine.StartReplay(r.Context(), v)
	if errors.Is(err, core.ErrReplayBusy) {
		problem(w, http.StatusConflict, err.Error())
		return
	}
	if err != nil {
		problem(w, 422, err.Error())
		return
	}
	write(w, 202, task)
}

// cancelReplay stops a running replay; it must reach the API instance that
// runs it, which a single-replica deployment always does.
func (s *Server) cancelReplay(w http.ResponseWriter, r *http.Request) {
	if devicescope.Limited(r.Context()) {
		problem(w, 404, "replay not found")
		return
	}
	if err := s.engine.CancelReplay(claims(r).TenantID, r.PathValue("id")); err != nil {
		problem(w, http.StatusConflict, err.Error())
		return
	}
	s.audit(r, "replay.cancel", "replay", r.PathValue("id"), nil)
	write(w, http.StatusAccepted, map[string]any{"cancelling": true})
}

func (s *Server) getReplay(w http.ResponseWriter, r *http.Request) {
	v, err := s.engine.Repo.GetReplay(r.Context(), r.PathValue("id"))
	if err != nil {
		problem(w, 404, "replay not found")
		return
	}
	// Only full-scope users can start a replay, so a limited user never owns one.
	if v.TenantID != claims(r).TenantID || devicescope.Limited(r.Context()) {
		problem(w, 404, "replay not found")
		return
	}
	write(w, 200, s.engine.RefreshReplay(r.Context(), v))
}
