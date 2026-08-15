package webbinding

import (
	"os"
	"testing"
)

// TestSessionStorePersistenceHardening covers the Task 13 store fixes:
// atomic write, debounced updates, corrupt-file quarantine.
func TestSessionStorePersistenceHardening(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/sessions.json"
	store, err := NewSessionStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Create(&BrowserSession{SessionID: "s1", Origin: "https://a", SubjectThumb: [32]byte{1}, CreatedAtMs: 1}); err != nil {
		t.Fatal(err)
	}
	// Lifecycle write flushed immediately.
	if _, err := os.Stat(path); err != nil {
		t.Fatal("create did not persist")
	}
	// High-frequency update marks dirty without immediate write cost.
	if err := store.AdvanceSequence("s1", 5); err != nil {
		t.Fatal(err)
	}
	store.Flush()
	reloaded, err := NewSessionStore(path)
	if err != nil {
		t.Fatal(err)
	}
	sess, ok := reloaded.Get("s1")
	if !ok || sess.LastSequence != 5 {
		t.Fatalf("reload: %+v ok=%v", sess, ok)
	}
	// Corrupt snapshot quarantined, not fatal.
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewSessionStore(path); err != nil {
		t.Fatalf("corrupt store must not brick startup: %v", err)
	}
	if _, err := os.Stat(path + ".corrupt"); err != nil {
		t.Fatal("corrupt snapshot not quarantined")
	}
}
