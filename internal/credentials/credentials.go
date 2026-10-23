package credentials

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

const refPrefix = "wincred://"

type Store interface {
	Save(target, username, password string) (string, error)
	Load(ref string) (string, error)
	Delete(ref string) error
}

type FileStore struct {
	path string
	mu   sync.Mutex
}

func NewFileStore(path string) *FileStore {
	return &FileStore{path: path}
}

func defaultCredentialsPath() string {
	dir, err := os.UserConfigDir()
	if err != nil || dir == "" {
		dir = "."
	}
	return filepath.Join(dir, "nacos-config-sync-tools", "credentials.json")
}

func (s *FileStore) Save(target, username, password string) (string, error) {
	if target == "" || username == "" {
		return "", errors.New("credential target and username are required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	entries, err := s.readAll()
	if err != nil {
		return "", err
	}
	entries[target] = password
	if err := s.writeAll(entries); err != nil {
		return "", err
	}
	return MakeRef(target, username), nil
}

func (s *FileStore) Load(ref string) (string, error) {
	target, _, err := ParseRef(ref)
	if err != nil {
		return "", err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	entries, err := s.readAll()
	if err != nil {
		return "", err
	}
	val, ok := entries[target]
	if !ok {
		return "", fmt.Errorf("credential not found for %q", target)
	}
	return val, nil
}

func (s *FileStore) Delete(ref string) error {
	target, _, err := ParseRef(ref)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	entries, err := s.readAll()
	if err != nil {
		return err
	}
	delete(entries, target)
	return s.writeAll(entries)
}

func (s *FileStore) readAll() (map[string]string, error) {
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]string{}, nil
	}
	if err != nil {
		return nil, err
	}
	var entries map[string]string
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, err
	}
	if entries == nil {
		entries = map[string]string{}
	}
	return entries, nil
}

func (s *FileStore) writeAll(entries map[string]string) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, data, 0o600)
}

func MakeRef(target, username string) string {
	return refPrefix + escapePart(target) + "/" + escapePart(username)
}

func ParseRef(ref string) (string, string, error) {
	if !strings.HasPrefix(ref, refPrefix) {
		return "", "", fmt.Errorf("unsupported credential reference %q", ref)
	}
	rest := strings.TrimPrefix(ref, refPrefix)
	parts := strings.SplitN(rest, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", errors.New("invalid credential reference")
	}
	return unescapePart(parts[0]), unescapePart(parts[1]), nil
}

func escapePart(value string) string {
	return strings.NewReplacer("%", "%25", "/", "%2F").Replace(value)
}

func unescapePart(value string) string {
	return strings.NewReplacer("%2F", "/", "%25", "%").Replace(value)
}
