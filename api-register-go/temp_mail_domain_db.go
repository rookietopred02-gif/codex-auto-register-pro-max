package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	bolt "go.etcd.io/bbolt"
)

const blockedTempMailDomainsBucket = "blocked_temp_mail_domains"

type tempMailDomainRecord struct {
	Domain    string `json:"domain"`
	Reason    string `json:"reason"`
	UpdatedAt string `json:"updated_at"`
}

type tempMailDomainDB struct {
	mu sync.RWMutex
	db *bolt.DB
}

var blockedTempMailDomainsDB *tempMailDomainDB

func openTempMailDomainDB(path string) (*tempMailDomainDB, error) {
	if strings.TrimSpace(path) == "" {
		return nil, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, err
	}
	db, err := bolt.Open(path, 0600, &bolt.Options{Timeout: time.Second})
	if err != nil {
		return nil, err
	}
	store := &tempMailDomainDB{db: db}
	if err := db.Update(func(tx *bolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists([]byte(blockedTempMailDomainsBucket))
		return err
	}); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func (s *tempMailDomainDB) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *tempMailDomainDB) MarkBlocked(domain, reason string) error {
	if s == nil || s.db == nil {
		return nil
	}
	domain = strings.TrimSpace(strings.ToLower(domain))
	if domain == "" {
		return nil
	}
	record := tempMailDomainRecord{
		Domain:    domain,
		Reason:    strings.TrimSpace(reason),
		UpdatedAt: time.Now().UTC().Format(time.RFC3339),
	}
	payload, err := json.Marshal(record)
	if err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	return s.db.Update(func(tx *bolt.Tx) error {
		bucket := tx.Bucket([]byte(blockedTempMailDomainsBucket))
		return bucket.Put([]byte(domain), payload)
	})
}

func (s *tempMailDomainDB) Snapshot() map[string]string {
	out := make(map[string]string)
	if s == nil || s.db == nil {
		return out
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	_ = s.db.View(func(tx *bolt.Tx) error {
		bucket := tx.Bucket([]byte(blockedTempMailDomainsBucket))
		if bucket == nil {
			return nil
		}
		return bucket.ForEach(func(k, v []byte) error {
			domain := strings.TrimSpace(strings.ToLower(string(k)))
			if domain == "" {
				return nil
			}
			var record tempMailDomainRecord
			if err := json.Unmarshal(v, &record); err == nil && strings.TrimSpace(record.Reason) != "" {
				out[domain] = strings.TrimSpace(record.Reason)
				return nil
			}
			out[domain] = ""
			return nil
		})
	})

	return out
}

func initBlockedTempMailDomainsDB(baseDir string) error {
	path := filepath.Join(strings.TrimSpace(baseDir), "temp_mail_state.db")
	store, err := openTempMailDomainDB(path)
	if err != nil {
		return err
	}
	blockedTempMailDomainsDB = store
	return nil
}

func closeBlockedTempMailDomainsDB() error {
	if blockedTempMailDomainsDB == nil {
		return nil
	}
	return blockedTempMailDomainsDB.Close()
}

func loadBlockedTempMailDomains() map[string]string {
	if blockedTempMailDomainsDB == nil {
		return make(map[string]string)
	}
	return blockedTempMailDomainsDB.Snapshot()
}

func persistBlockedTempMailDomain(domain, reason string) error {
	if blockedTempMailDomainsDB == nil {
		return nil
	}
	return blockedTempMailDomainsDB.MarkBlocked(domain, reason)
}
