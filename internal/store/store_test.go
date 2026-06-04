package store

import (
	"path/filepath"
	"testing"
	"time"

	"go.etcd.io/bbolt"
)

// newTestStore returns a SecretsStore backed by a temporary bbolt DB with the
// expected buckets, for exercising the persistence methods in isolation.
func newTestStore(t *testing.T) *SecretsStore {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	db, err := bbolt.Open(path, 0o600, &bbolt.Options{Timeout: 2 * time.Second})
	if err != nil {
		t.Fatalf("open bbolt: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Update(func(tx *bbolt.Tx) error {
		for _, name := range []string{"Tracks", "Settings", "Cache"} {
			if _, err := tx.CreateBucketIfNotExists([]byte(name)); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("create buckets: %v", err)
	}
	return &SecretsStore{db: db}
}

type histTrack struct {
	ID    int    `json:"id"`
	Title string `json:"title"`
}

func TestSaveLoadHistory(t *testing.T) {
	s := newTestStore(t)

	// Empty load is a no-op (target stays nil).
	var empty []histTrack
	if err := s.LoadHistory(&empty); err != nil {
		t.Fatalf("LoadHistory (empty): %v", err)
	}
	if empty != nil {
		t.Errorf("expected nil history before any save, got %+v", empty)
	}

	want := []histTrack{{ID: 1, Title: "A"}, {ID: 2, Title: "B"}}
	if err := s.SaveHistory(want); err != nil {
		t.Fatalf("SaveHistory: %v", err)
	}
	var got []histTrack
	if err := s.LoadHistory(&got); err != nil {
		t.Fatalf("LoadHistory: %v", err)
	}
	if len(got) != 2 || got[0].ID != 1 || got[1].Title != "B" {
		t.Errorf("round-trip mismatch: %+v", got)
	}
}

func TestSaveLoadTheme(t *testing.T) {
	s := newTestStore(t)

	if name, err := s.LoadTheme(); err != nil || name != "" {
		t.Fatalf("expected empty theme before save, got %q (err %v)", name, err)
	}
	if err := s.SaveTheme("rosepine"); err != nil {
		t.Fatalf("SaveTheme: %v", err)
	}
	name, err := s.LoadTheme()
	if err != nil {
		t.Fatalf("LoadTheme: %v", err)
	}
	if name != "rosepine" {
		t.Errorf("expected rosepine, got %q", name)
	}
}

// A nil-db store must no-op rather than panic (used by render-only tests).
func TestNilDBStoreNoop(t *testing.T) {
	s := &SecretsStore{}
	if err := s.SaveHistory([]histTrack{{ID: 1}}); err != nil {
		t.Errorf("SaveHistory on nil db should no-op, got %v", err)
	}
	var got []histTrack
	if err := s.LoadHistory(&got); err != nil {
		t.Errorf("LoadHistory on nil db should no-op, got %v", err)
	}
	if got != nil {
		t.Errorf("expected nil from nil-db load, got %+v", got)
	}
}
