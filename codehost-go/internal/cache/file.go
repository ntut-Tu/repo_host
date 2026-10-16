package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

type File struct {
	directory string
	mu        sync.Mutex
	lastPrune time.Time
}

type entry struct {
	Expires time.Time       `json:"expires"`
	Value   json.RawMessage `json:"value"`
}

func New(directory string) (*File, error) {
	if err := os.MkdirAll(directory, 0700); err != nil {
		return nil, err
	}
	return &File{directory: directory}, nil
}

func (f *File) path(kind, key string) string {
	sum := sha256.Sum256([]byte(kind + "\x00" + key))
	return filepath.Join(f.directory, hex.EncodeToString(sum[:])+".json")
}

func (f *File) Get(kind, key string, target any) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	lock, err := f.lock()
	if err != nil {
		return false
	}
	defer f.unlock(lock)
	path := f.path(kind, key)
	contents, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	var record entry
	if json.Unmarshal(contents, &record) != nil || !time.Now().Before(record.Expires) {
		_ = os.Remove(path)
		return false
	}
	return json.Unmarshal(record.Value, target) == nil
}

func (f *File) Put(kind, key string, value any, ttl time.Duration) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	lock, err := f.lock()
	if err != nil {
		return err
	}
	defer f.unlock(lock)
	payload, err := json.Marshal(value)
	if err != nil {
		return err
	}
	contents, err := json.Marshal(entry{Expires: time.Now().Add(ttl), Value: payload})
	if err != nil {
		return err
	}
	temp, err := os.CreateTemp(f.directory, ".pending-*")
	if err != nil {
		return err
	}
	defer os.Remove(temp.Name())
	if _, err = temp.Write(contents); err != nil {
		_ = temp.Close()
		return err
	}
	if err = temp.Close(); err != nil {
		return err
	}
	if err := os.Rename(temp.Name(), f.path(kind, key)); err != nil {
		return err
	}
	if time.Since(f.lastPrune) >= time.Minute {
		f.pruneExpired()
		f.lastPrune = time.Now()
	}
	return nil
}

func (f *File) lock() (*os.File, error) {
	file, err := os.OpenFile(filepath.Join(f.directory, ".lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX); err != nil {
		_ = file.Close()
		return nil, err
	}
	return file, nil
}

func (f *File) unlock(file *os.File) {
	_ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
	_ = file.Close()
}

func (f *File) pruneExpired() {
	files, err := os.ReadDir(f.directory)
	if err != nil {
		return
	}
	for _, file := range files {
		if file.IsDir() || !strings.HasSuffix(file.Name(), ".json") {
			continue
		}
		path := filepath.Join(f.directory, file.Name())
		contents, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var record entry
		if json.Unmarshal(contents, &record) != nil || !time.Now().Before(record.Expires) {
			_ = os.Remove(path)
		}
	}
}
