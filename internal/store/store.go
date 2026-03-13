package store

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/docker/secrets-engine/store"
	"github.com/docker/secrets-engine/store/keychain"
	"github.com/docker/secrets-engine/store/posixage"
	"github.com/docker/secrets-engine/x/secrets"
	"go.etcd.io/bbolt"
)

const (
	ServiceName = "tidal-tui"
	AccountName = "session"
	DBFile      = "tidal-cache.db"
)

func dbPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return DBFile
	}
	dir := filepath.Join(home, ".local", "share", ServiceName)
	os.MkdirAll(dir, 0700)
	return filepath.Join(dir, DBFile)
}

// SecretsStore handles secure storage using the docker/secrets-engine keychain or posixage fallback.
type SecretsStore struct {
	store store.Store
	db    *bbolt.DB
}

// tidalSecret implements the store.Secret interface
type tidalSecret struct {
	Data []byte
}

func (s *tidalSecret) Marshal() ([]byte, error) { return s.Data, nil }
func (s *tidalSecret) Unmarshal(data []byte) error {
	s.Data = data
	return nil
}
func (s *tidalSecret) Metadata() map[string]string { return nil }
func (s *tidalSecret) SetMetadata(meta map[string]string) error { return nil }

func tidalSecretFactory(ctx context.Context, id store.ID) *tidalSecret {
	return &tidalSecret{}
}

func NewSecretsStore() *SecretsStore {
	var s store.Store
	var err error

	// 1. Try Keychain
	s, err = keychain.New[*tidalSecret](ServiceName, AccountName, tidalSecretFactory)
	if err != nil {
		fmt.Printf("Warning: failed to initialize keychain: %v. Falling back to posixage.\n", err)
		
		// 2. Fallback to Posixage
		home, _ := os.UserHomeDir()
		storePath := filepath.Join(home, ".config", ServiceName, "secrets")
		os.MkdirAll(storePath, 0700)
		
		root, rErr := os.OpenRoot(storePath)
		if rErr != nil {
			fmt.Printf("Error: failed to open root for posixage: %v\n", rErr)
		} else {
			s, err = posixage.New[*tidalSecret](root, tidalSecretFactory)
			if err != nil {
				fmt.Printf("Error: failed to initialize posixage: %v\n", err)
			}
		}
	}

	db, err := bbolt.Open(dbPath(), 0600, &bbolt.Options{Timeout: 5 * time.Second})
	if err != nil {
		fmt.Printf("Warning: failed to open bolt db: %v\n", err)
	} else {
		db.Update(func(tx *bbolt.Tx) error {
			if _, err := tx.CreateBucketIfNotExists([]byte("Tracks")); err != nil {
				return err
			}
			_, err := tx.CreateBucketIfNotExists([]byte("Settings"))
			return err
		})
	}

	return &SecretsStore{store: s, db: db}
}

func (s *SecretsStore) SaveSession(data interface{}) error {
	if s.store == nil { return fmt.Errorf("no secure store initialized") }
	bytes, err := json.Marshal(data)
	if err != nil { return err }
	return s.store.Upsert(context.Background(), secrets.MustParseID(AccountName), &tidalSecret{Data: bytes})
}

func (s *SecretsStore) LoadSession(target interface{}) error {
	if s.store == nil { return fmt.Errorf("no secure store initialized") }
	secret, err := s.store.Get(context.Background(), secrets.MustParseID(AccountName))
	if err != nil { return err }
	bytes, err := secret.Marshal()
	if err != nil { return err }
	return json.Unmarshal(bytes, target)
}

func (s *SecretsStore) CacheTrack(trackID int, data interface{}) error {
	if s.db == nil { return nil }
	return s.db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte("Tracks"))
		bytes, err := json.Marshal(data)
		if err != nil { return err }
		return b.Put([]byte(fmt.Sprintf("%d", trackID)), bytes)
	})
}

func (s *SecretsStore) SaveDevice(hwName string) error {
	if s.db == nil {
		return nil
	}
	return s.db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte("Settings"))
		if b == nil {
			return nil
		}
		return b.Put([]byte("device"), []byte(hwName))
	})
}

func (s *SecretsStore) LoadDevice() (string, error) {
	if s.db == nil {
		return "", nil
	}
	var device string
	err := s.db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte("Settings"))
		if b == nil {
			return nil
		}
		v := b.Get([]byte("device"))
		if v != nil {
			device = string(v)
		}
		return nil
	})
	return device, err
}

func (s *SecretsStore) SaveVolume(vol float64) error {
	if s.db == nil {
		return nil
	}
	return s.db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte("Settings"))
		if b == nil {
			return nil
		}
		return b.Put([]byte("volume"), []byte(fmt.Sprintf("%f", vol)))
	})
}

func (s *SecretsStore) LoadVolume() (float64, error) {
	if s.db == nil {
		return 100.0, nil
	}
	vol := 100.0
	err := s.db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte("Settings"))
		if b == nil {
			return nil
		}
		v := b.Get([]byte("volume"))
		if v == nil {
			return nil
		}
		_, err := fmt.Sscanf(string(v), "%f", &vol)
		return err
	})
	return vol, err
}

func (s *SecretsStore) Close() {
	if s.db != nil {
		s.db.Close()
	}
}
