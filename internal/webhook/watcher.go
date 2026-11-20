package webhook

import (
	"context"
	"log/slog"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// Watcher monitors webhooks.yaml for filesystem modifications.
type Watcher struct {
	targetPath string
	targetBase string
	onReload   func()
	watcher    *fsnotify.Watcher
	stopCh     chan struct{}
	loopWg     sync.WaitGroup
	stopOnce   sync.Once
}

// NewWatcher creates and starts an fsnotify watcher on targetPath's directory.
func NewWatcher(targetPath string, onReload func()) (*Watcher, error) {
	cleanPath, err := filepath.Abs(targetPath)
	if err != nil {
		cleanPath = targetPath
	}
	dir := filepath.Dir(cleanPath)
	base := filepath.Base(cleanPath)

	fw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	if err := fw.Add(dir); err != nil {
		fw.Close()
		return nil, err
	}

	w := &Watcher{
		targetPath: cleanPath,
		targetBase: base,
		onReload:   onReload,
		watcher:    fw,
		stopCh:     make(chan struct{}),
	}

	return w, nil
}

// Start begins the event loop in a background goroutine.
func (w *Watcher) Start(ctx context.Context) {
	w.loopWg.Add(1)
	go func() {
		defer w.loopWg.Done()
		w.run(ctx)
	}()
}

// run executes the event loop until ctx is canceled or Stop() is called.
func (w *Watcher) run(ctx context.Context) {
	var (
		debounceTimer *time.Timer
		debounceCh    <-chan time.Time
	)

	triggerReload := func() {
		if debounceTimer == nil {
			debounceTimer = time.NewTimer(100 * time.Millisecond)
			debounceCh = debounceTimer.C
			return
		}
		if !debounceTimer.Stop() {
			select {
			case <-debounceTimer.C:
			default:
			}
		}
		debounceTimer.Reset(100 * time.Millisecond)
		debounceCh = debounceTimer.C
	}

	defer func() {
		if debounceTimer != nil {
			debounceTimer.Stop()
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case <-w.stopCh:
			return
		case <-debounceCh:
			debounceCh = nil
			if w.onReload != nil {
				w.onReload()
			}
		case event, ok := <-w.watcher.Events:
			if !ok {
				return
			}
			// Only react to events on the targeted filename
			if filepath.Base(event.Name) == w.targetBase {
				if event.Has(fsnotify.Write) || event.Has(fsnotify.Create) || event.Has(fsnotify.Rename) {
					triggerReload()
				}
			}
		case err, ok := <-w.watcher.Errors:
			if !ok {
				return
			}
			slog.Warn("webhook watcher error", "error", err)
		}
	}
}

// Stop cleanly terminates the watcher.
func (w *Watcher) Stop() {
	w.stopOnce.Do(func() {
		close(w.stopCh)
		if w.watcher != nil {
			_ = w.watcher.Close()
		}
	})
	w.loopWg.Wait()
}
