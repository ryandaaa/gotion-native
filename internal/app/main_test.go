package app

import (
	"errors"
	"gotion/internal/tr"
	"os"
	"sync"
	"testing"

	"gotion/internal/notion"
)

// memSecrets keeps credentials in memory for the tests: none of them may
// read or write the user's keychain.
type memSecrets struct {
	mu sync.Mutex
	m  map[string][]byte
}

func (s *memSecrets) Get(k string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if v, ok := s.m[k]; ok {
		return v, nil
	}
	return nil, errors.New("not found")
}

func (s *memSecrets) Set(k string, v []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[k] = v
	return nil
}

func (s *memSecrets) Delete(k string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.m, k)
	return nil
}

func TestMain(m *testing.M) {
	secrets = &memSecrets{m: map[string][]byte{}}
	// Nor Notion itself: a client a test does not point at a fake goes
	// nowhere.
	notion.DefaultBase = "http://127.0.0.1:9/v1"
	// Nor the user's settings, cache or token file.
	dir, _ := os.MkdirTemp("", "gotion-test")
	os.Setenv("XDG_CONFIG_HOME", dir)
	os.Setenv("HOME", dir)
	os.Unsetenv("NOTION_TOKEN")
	// The tests read gotion's texts in Indonesian, its own language.
	PreferredLanguages = func() []string { return []string{"id"} }
	tr.Set(tr.ID)
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}
