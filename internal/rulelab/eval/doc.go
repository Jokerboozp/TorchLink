// Package eval contains rule predicates and lifecycle intentions shared by the
// production adapter and isolated historical experiments. It has no repository,
// bus, realtime publisher, device control or wall-clock dependency.
//
// ProcessingV1 preserves the inspected production semantics: duration uses
// processing seconds and requires a later matching message, trigger wins over
// recovery, rule auto-recovery only visits ACTIVE, and ordinary alarm upserts
// retain the original TriggerID. Callers supply each actually observed stage
// time; message event time cannot silently replace an absent processing clock.
// RevisionV2 adds immutable producing/trigger revisions, revision-scoped pending,
// and retention of unresolved alarms on disable/delete. Their recovery uses the
// producing revision. The adapter performs activation CAS and side effects.
package eval
