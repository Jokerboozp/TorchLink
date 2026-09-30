package alarmgovernance

import (
	"errors"
	"testing"
	"time"
)

func TestOrphanCleanupSurvivesFailureAndKeepsFormalReferencedObjects(t *testing.T) {
	f := setup(t)
	parent := f.newVerification(t)
	archive := newTestArchive()
	archive.putErr = errors.New("remote failure after object write")
	if _, e := f.s.UploadAttachment(f.ctx, f.a, archive, parent.Kind, parent.ID, "pending.pdf", "pending", pdfEvidence); e == nil {
		t.Fatal("failed upload accepted")
	}
	archive.putErr = nil
	attached, e := f.s.UploadAttachment(f.ctx, f.a, archive, parent.Kind, parent.ID, "formal.pdf", "formal", pdfEvidence)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.s.WithdrawAttachment(f.ctx, f.a, attached.ID, attached.Version, "retain audit"); e != nil {
		t.Fatal(e)
	}
	if n, e := f.s.CleanupAttachmentUploads(f.ctx, f.a.TenantID, archive); e != nil || n != 0 {
		t.Fatal("grace not respected", n, e)
	}
	f.now += int64((11 * time.Minute) / time.Millisecond)
	if n, e := f.s.CleanupAttachmentUploads(f.ctx, f.a.TenantID, archive); e != nil || n != 1 {
		t.Fatal("pending compensation not recovered", n, e)
	}
	if len(archive.objects) != 1 {
		t.Fatal("referenced withdrawn object deleted", archive.objects)
	}
	if n, e := f.s.CleanupAttachmentUploads(f.ctx, f.a.TenantID, archive); e != nil || n != 0 {
		t.Fatal("completed cleanup repeated", n, e)
	}
}
