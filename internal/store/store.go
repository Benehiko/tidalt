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

// PassphraseFunc is called to obtain a passphrase for encrypting or decrypting
// the age-encrypted fallback store. The prompt string describes what is being
// asked (e.g. "Enter passphrase" vs "Confirm passphrase").
type PassphraseFunc func(ctx context.Context, prompt string) ([]byte, error)

const (
	ServiceName = "tidalt"
	AccountName = "session"
	DBFile      = "tidal-cache.db"
)

func dbPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return DBFile
	}
	dir := filepath.Join(home, ".local", "share", ServiceName)
	_ = os.MkdirAll(dir, 0o700)
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
func (s *tidalSecret) Metadata() map[string]string              { return nil }
func (s *tidalSecret) SetMetadata(meta map[string]string) error { return nil }

func tidalSecretFactory(ctx context.Context, id store.ID) *tidalSecret {
	return &tidalSecret{}
}

// NewClientStore opens only the secrets backend (keychain / posixage) without
// the bbolt database. Use this in client mode where the parent process already
// holds the exclusive DB lock.
func NewClientStore(passphrase PassphraseFunc) *SecretsStore {
	var s store.Store
	var err error

	s, err = keychain.New(ServiceName, AccountName, tidalSecretFactory)
	if err != nil {
		home, _ := os.UserHomeDir()
		storePath := filepath.Join(home, ".config", ServiceName, "secrets")
		_ = os.MkdirAll(storePath, 0o700)

		root, rErr := os.OpenRoot(storePath)
		if rErr != nil {
			fmt.Printf("Error: failed to open root for posixage: %v\n", rErr)
		} else {
			encryptFn := posixage.EncryptionPassword(func(ctx context.Context) ([]byte, error) {
				return passphrase(ctx, "Enter passphrase for secret store")
			})
			decryptFn := posixage.DecryptionPassword(func(ctx context.Context) ([]byte, error) {
				return passphrase(ctx, "Enter passphrase for secret store")
			})
			s, err = posixage.New(root, tidalSecretFactory,
				posixage.WithEncryptionCallbackFunc(encryptFn),
				posixage.WithDecryptionCallbackFunc(decryptFn),
			)
			if err != nil {
				fmt.Printf("Error: failed to initialize posixage: %v\n", err)
			}
		}
	}

	return &SecretsStore{store: s}
}

func NewSecretsStore(passphrase PassphraseFunc) *SecretsStore {
	var s store.Store
	var err error

	// 1. Try Keychain
	s, err = keychain.New(ServiceName, AccountName, tidalSecretFactory)
	if err != nil {
		fmt.Printf("Warning: failed to initialize keychain: %v. Falling back to posixage.\n", err)

		// 2. Fallback to Posixage — requires encryption/decryption callbacks.
		home, _ := os.UserHomeDir()
		storePath := filepath.Join(home, ".config", ServiceName, "secrets")
		_ = os.MkdirAll(storePath, 0o700)

		root, rErr := os.OpenRoot(storePath)
		if rErr != nil {
			fmt.Printf("Error: failed to open root for posixage: %v\n", rErr)
		} else {
			encryptFn := posixage.EncryptionPassword(func(ctx context.Context) ([]byte, error) {
				return passphrase(ctx, "Enter passphrase for secret store")
			})
			decryptFn := posixage.DecryptionPassword(func(ctx context.Context) ([]byte, error) {
				return passphrase(ctx, "Enter passphrase for secret store")
			})
			s, err = posixage.New(root, tidalSecretFactory,
				posixage.WithEncryptionCallbackFunc(encryptFn),
				posixage.WithDecryptionCallbackFunc(decryptFn),
			)
			if err != nil {
				fmt.Printf("Error: failed to initialize posixage: %v\n", err)
			}
		}
	}

	db, err := bbolt.Open(dbPath(), 0o600, &bbolt.Options{Timeout: 5 * time.Second})
	if err != nil {
		fmt.Printf("Warning: failed to open bolt db: %v\n", err)
	} else {
		if err := db.Update(func(tx *bbolt.Tx) error {
			for _, name := range []string{"Tracks", "Settings", "Cache"} {
				if _, err := tx.CreateBucketIfNotExists([]byte(name)); err != nil {
					return err
				}
			}
			return nil
		}); err != nil {
			fmt.Printf("Warning: failed to initialize bolt db buckets: %v\n", err)
		}
	}

	return &SecretsStore{store: s, db: db}
}

func (s *SecretsStore) SaveSession(data any) error {
	if s.store == nil {
		return fmt.Errorf("no secure store initialized")
	}
	bytes, err := json.Marshal(data)
	if err != nil {
		return err
	}
	return s.store.Upsert(context.Background(), secrets.MustParseID(AccountName), &tidalSecret{Data: bytes})
}

func (s *SecretsStore) LoadSession(target any) error {
	if s.store == nil {
		return fmt.Errorf("no secure store initialized")
	}
	secret, err := s.store.Get(context.Background(), secrets.MustParseID(AccountName))
	if err != nil {
		return err
	}
	bytes, err := secret.Marshal()
	if err != nil {
		return err
	}
	return json.Unmarshal(bytes, target)
}

func (s *SecretsStore) DeleteSession() error {
	if s.store == nil {
		return fmt.Errorf("no secure store initialized")
	}
	return s.store.Delete(context.Background(), secrets.MustParseID(AccountName))
}

func (s *SecretsStore) CacheTrack(trackID int, data any) error {
	if s.db == nil {
		return nil
	}
	return s.db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte("Tracks"))
		bytes, err := json.Marshal(data)
		if err != nil {
			return err
		}
		return b.Put(fmt.Appendf(nil, "%d", trackID), bytes)
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
		return b.Put([]byte("volume"), fmt.Appendf(nil, "%f", vol))
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

func (s *SecretsStore) SaveLastPosition(seconds float64) error {
	if s.db == nil {
		return nil
	}
	return s.db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte("Settings"))
		if b == nil {
			return nil
		}
		return b.Put([]byte("lastPosition"), fmt.Appendf(nil, "%f", seconds))
	})
}

func (s *SecretsStore) LoadLastPosition() (float64, error) {
	if s.db == nil {
		return 0, nil
	}
	var pos float64
	err := s.db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte("Settings"))
		if b == nil {
			return nil
		}
		v := b.Get([]byte("lastPosition"))
		if v == nil {
			return nil
		}
		_, err := fmt.Sscanf(string(v), "%f", &pos)
		return err
	})
	return pos, err
}

func (s *SecretsStore) SaveLastTrackID(trackID int) error {
	if s.db == nil {
		return nil
	}
	return s.db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte("Settings"))
		if b == nil {
			return nil
		}
		return b.Put([]byte("lastTrackID"), fmt.Appendf(nil, "%d", trackID))
	})
}

func (s *SecretsStore) LoadLastTrackID() (int, error) {
	if s.db == nil {
		return 0, nil
	}
	var id int
	err := s.db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte("Settings"))
		if b == nil {
			return nil
		}
		v := b.Get([]byte("lastTrackID"))
		if v == nil {
			return nil
		}
		_, err := fmt.Sscanf(string(v), "%d", &id)
		return err
	})
	return id, err
}

// SavePlaylist persists the current track list so it can be restored on next
// startup.
func (s *SecretsStore) SavePlaylist(tracks any) error {
	if s.db == nil {
		return nil
	}
	return s.db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte("Settings"))
		if b == nil {
			return nil
		}
		data, err := json.Marshal(tracks)
		if err != nil {
			return err
		}
		return b.Put([]byte("playlist"), data)
	})
}

// LoadPlaylist restores the track list saved by the previous session.
// Returns nil, nil when no playlist is stored yet.
func (s *SecretsStore) LoadPlaylist(target any) error {
	if s.db == nil {
		return nil
	}
	return s.db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte("Settings"))
		if b == nil {
			return nil
		}
		v := b.Get([]byte("playlist"))
		if v == nil {
			return nil
		}
		return json.Unmarshal(v, target)
	})
}

// CacheSearchResults stores the tracks returned for a search query so they can
// be served from cache on repeated lookups.
func (s *SecretsStore) CacheSearchResults(query string, tracks any) error {
	if s.db == nil {
		return nil
	}
	return s.db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte("Cache"))
		if b == nil {
			return nil
		}
		data, err := json.Marshal(tracks)
		if err != nil {
			return err
		}
		return b.Put([]byte("search:"+query), data)
	})
}

// LoadSearchResults retrieves cached results for query. Returns false when
// there is no cached entry.
func (s *SecretsStore) LoadSearchResults(query string, target any) (bool, error) {
	if s.db == nil {
		return false, nil
	}
	var found bool
	err := s.db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte("Cache"))
		if b == nil {
			return nil
		}
		v := b.Get([]byte("search:" + query))
		if v == nil {
			return nil
		}
		found = true
		return json.Unmarshal(v, target)
	})
	return found, err
}

func (s *SecretsStore) Close() {
	if s.db != nil {
		_ = s.db.Close()
	}
}
