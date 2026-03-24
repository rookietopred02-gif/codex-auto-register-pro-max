package main

import (
	"path/filepath"
	"testing"
)

func TestTempMailDomainDBPersistsBlockedDomains(t *testing.T) {
	store, err := openTempMailDomainDB(filepath.Join(t.TempDir(), "temp_mail_state.db"))
	if err != nil {
		t.Fatalf("openTempMailDomainDB() error = %v", err)
	}
	defer store.Close()

	if err := store.MarkBlocked("ShareBot.NET", "registration_disallowed"); err != nil {
		t.Fatalf("MarkBlocked() error = %v", err)
	}

	snapshot := store.Snapshot()
	if got := snapshot["sharebot.net"]; got != "registration_disallowed" {
		t.Fatalf("snapshot reason = %q, want registration_disallowed", got)
	}
}
