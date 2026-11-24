package app

import (
	"errors"
	"os"
	"sync"
	"testing"
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
	// Nor the user's settings, cache or token file.
	dir, _ := os.MkdirTemp("", "gotion-test")
	os.Setenv("XDG_CONFIG_HOME", dir)
	os.Setenv("HOME", dir)
	os.Unsetenv("NOTION_TOKEN")
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}
