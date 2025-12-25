package backup

import (
	"testing"
)

// Since Status logic heavily depends on comparing file system state with BackupDirectory,
// it involves significant setup.
// For unit tests, we can test StatusReport formatting or logic if we can mock DirectoryEntry.
// Given the current architecture, full integration-like unit tests might be easier given dependencies.
// Let's create a simple test that exercises the report formatting for now,
// as mocking filesystem and backup directory structures is complex without mocks.

func TestStatusReport_Counters(t *testing.T) {
	report := NewStatusReport()
	report.Counters[StatusNew]++
	report.Counters[StatusArchived]++
	report.Counters[StatusArchivedContentMissing]++

	if report.Counters[StatusNew] != 1 {
		t.Error("Expected 1 New status")
	}
}
