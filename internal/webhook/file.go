package webhook

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/google/uuid"
	"gopkg.in/yaml.v3"
)

// FileManager manages thread-safe, atomic disk operations on webhooks.yaml.
type FileManager struct {
	path string
	mu   sync.RWMutex
}

// NewFileManager creates a new FileManager targeting path.
func NewFileManager(path string) *FileManager {
	return &FileManager{path: path}
}

// Path returns the targeted file path.
func (f *FileManager) Path() string {
	return f.path
}

// Load reads and parses all webhook rules from disk.
// If the file does not exist, it returns an empty slice and no error.
func (f *FileManager) Load() ([]WebhookRule, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()

	data, err := os.ReadFile(f.path)
	if err != nil {
		if os.IsNotExist(err) {
			return []WebhookRule{}, nil
		}
		return nil, fmt.Errorf("webhook: read file %q: %w", f.path, err)
	}

	var wf WebhooksFile
	if err := yaml.Unmarshal(data, &wf); err != nil {
		return nil, fmt.Errorf("webhook: parse yaml %q: %w", f.path, err)
	}

	for i := range wf.Webhooks {
		if err := wf.Webhooks[i].Validate(); err != nil {
			return nil, fmt.Errorf("webhook %q: %w", wf.Webhooks[i].ID, err)
		}
	}

	return wf.Webhooks, nil
}

// Save writes rules atomically to disk. If atomic rename fails (e.g. when the
// target file is a Docker bind mount or its parent directory is on a read-only
// filesystem), it falls back to a direct write to the file.
func (f *FileManager) Save(rules []WebhookRule) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	wf := WebhooksFile{Webhooks: rules}
	data, err := yaml.Marshal(&wf)
	if err != nil {
		return fmt.Errorf("webhook: marshal yaml: %w", err)
	}

	dir := filepath.Dir(f.path)
	_ = os.MkdirAll(dir, 0755)

	// Attempt atomic save via temporary file in the same directory.
	tmpPath := filepath.Join(dir, fmt.Sprintf(".webhooks.%s.tmp", uuid.New().String()))
	tmpFile, err := os.OpenFile(tmpPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	if err == nil {
		writeErr := func() error {
			if _, err := tmpFile.Write(data); err != nil {
				_ = tmpFile.Close()
				return err
			}
			if err := tmpFile.Sync(); err != nil {
				_ = tmpFile.Close()
				return err
			}
			return tmpFile.Close()
		}()

		if writeErr == nil {
			if err := os.Rename(tmpPath, f.path); err == nil {
				return nil
			}
		}
		_ = os.Remove(tmpPath)
	}

	// Fallback to direct write (handles bind-mounted files where rename yields EBUSY
	// or where the parent directory is read-only).
	targetFile, err := os.OpenFile(f.path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		return fmt.Errorf("webhook: write file %q: %w", f.path, err)
	}
	defer targetFile.Close()

	if _, err := targetFile.Write(data); err != nil {
		return fmt.Errorf("webhook: write file %q: %w", f.path, err)
	}
	if err := targetFile.Sync(); err != nil {
		return fmt.Errorf("webhook: sync file %q: %w", f.path, err)
	}

	return nil
}
