package externaldata

import "context"

const failedStatuses = "FAILED,WAITING_BINDING,CONFLICT"
const pendingStatuses = "PENDING,RETRY,RUNNING"

func (s *Service) recordCounts(ctx context.Context, q Query) (processed, failed, pending int, err error) {
	for i, status := range []string{"PROCESSED,DUPLICATE,IGNORED,FILTERED", failedStatuses, pendingStatuses} {
		q.Status = status
		_, count, e := s.Store.List(ctx, q)
		if e != nil {
			return 0, 0, 0, e
		}
		switch i {
		case 0:
			processed = count
		case 1:
			failed = count
		case 2:
			pending = count
		}
	}
	return
}

func (s *Service) summary(ctx context.Context, tenant, source, endpoint string) (*RuntimeSummary, error) {
	v := &RuntimeSummary{}
	q := Query{TenantID: tenant, SourceID: source, EndpointID: endpoint, Limit: 1, UpdatedOrder: true}
	for _, part := range []struct{ kind, status string }{{"receipt", ""}, {"record", "PROCESSED,DUPLICATE"}, {"job", "COMPLETED"}, {"record", failedStatuses}, {"record", pendingStatuses}, {"job", "FAILED,RETRY"}} {
		q.Kind, q.Status = part.kind, part.status
		// Retrying an older receipt changes UpdatedAt, not when it arrived.
		q.UpdatedOrder = part.kind != "receipt"
		rows, total, err := s.Store.List(ctx, q)
		if err != nil {
			return nil, err
		}
		if part.status == failedStatuses {
			v.FailedRecords = total
		}
		if part.status == pendingStatuses {
			v.PendingRecords = total
		}
		if len(rows) == 0 {
			continue
		}
		e := rows[0]
		switch {
		case part.kind == "receipt":
			v.LastReceivedAt = e.CreatedAt
		case part.status == "PROCESSED,DUPLICATE":
			v.LastProcessedAt = e.UpdatedAt
		case part.status == "COMPLETED":
			v.LastPullAt = e.UpdatedAt
		case part.status == failedStatuses:
			if r, err := read[Record](e); err == nil {
				v.LastError = r.Error
				v.LastErrorAt = e.UpdatedAt
			}
		case part.kind == "job" && e.UpdatedAt > v.LastErrorAt:
			if j, err := read[Job](e); err == nil && j.Error != "" {
				v.LastError = j.Error
				v.LastErrorAt = e.UpdatedAt
			}
		}
	}
	return v, nil
}
