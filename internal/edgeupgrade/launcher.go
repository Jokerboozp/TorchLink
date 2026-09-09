package edgeupgrade

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"iot-platform/internal/edgeagent"
	"iot-platform/internal/model"
	"iot-platform/internal/protocolcatalog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

func binaryHash(path string) (string, error) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		return "", errors.New("program is not a regular file")
	}
	data, err := readFile(path, protocolcatalog.MaxSource)
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:]), nil
}

func (l *Launcher) startCurrent(ctx context.Context) error {
	hash, err := binaryHash(l.state.CurrentPath)
	if err != nil {
		return err
	}
	if l.state.CurrentSHA256 != "" && l.state.CurrentSHA256 != hash {
		return errors.New("installed program SHA-256 differs from local journal")
	}
	child, version, err := l.start(ctx, l.state.CurrentPath, l.state.CurrentVersion)
	if err != nil {
		return err
	}
	l.child = child
	l.state.CurrentVersion, l.state.CurrentSHA256 = version, hash
	return l.save()
}

// Run is a single-owner process supervisor. A candidate is committed only
// after a real child configuration fetch and authenticated platform heartbeat.
func (l *Launcher) Run(ctx context.Context) error {
	defer func() {
		if l.child != nil {
			l.child.stop()
		}
	}()
	var target model.EdgeProgram
	if err := l.request(ctx, "GET", nil, &target); err != nil {
		return err
	}
	if err := l.startCurrent(ctx); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if fallback := l.rollback(ctx, target.Generation, err); fallback != nil {
			return fallback
		}
	}
	tick := time.NewTicker(l.options.PollInterval)
	defer tick.Stop()
	targetValid := true
	for {
		if targetValid {
			if err := l.reconcile(ctx, target); err != nil {
				return err
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-l.child.done:
			if err := l.rollback(ctx, l.state.AppliedGeneration, errors.New("active program exited")); err != nil {
				return err
			}
		case <-tick.C:
		}
		if err := l.request(ctx, "GET", nil, &target); err != nil {
			if errors.Is(err, ErrUnauthorized) {
				return err
			}
			// Continue the active child through transient control-plane failures.
			// Do not apply a cached target without a fresh authenticated request.
			targetValid = false
		} else {
			targetValid = true
		}
	}
}

func (l *Launcher) rollback(ctx context.Context, generation int64, reason error) error {
	if l.child != nil {
		l.child.stop()
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	var authenticated model.EdgeProgram
	if err := l.request(ctx, "GET", nil, &authenticated); err != nil {
		return err
	}
	if l.state.PreviousPath == "" {
		_ = l.report(ctx, "FAILED", "", generation, reason.Error())
		return reason
	}
	l.state.CurrentPath, l.state.CurrentVersion, l.state.CurrentSHA256 = l.state.PreviousPath, l.state.PreviousVersion, l.state.PreviousSHA256
	l.state.PreviousPath, l.state.PreviousVersion, l.state.PreviousSHA256 = "", "", ""
	l.state.FailedGeneration, l.state.LastError, l.state.LastPhase = generation, reason.Error(), "ROLLED_BACK"
	if err := l.startCurrent(ctx); err != nil {
		_ = l.report(ctx, "FAILED", "", generation, err.Error())
		return fmt.Errorf("previous program could not resume: %w", err)
	}
	return l.report(ctx, "ROLLED_BACK", "", generation, reason.Error())
}

func (l *Launcher) reconcile(ctx context.Context, target model.EdgeProgram) error {
	if target.TargetVersion != "" && target.TargetVersion == l.state.CurrentVersion && target.Generation != l.state.FailedGeneration && target.Generation != l.state.AppliedGeneration {
		l.state.AppliedGeneration = target.Generation
		l.state.FailedGeneration, l.state.LastError, l.state.LastPhase = 0, "", "RUNNING"
		if err := l.save(); err != nil {
			return err
		}
	}
	if target.TargetVersion == "" || target.Generation == l.state.FailedGeneration || target.TargetVersion == l.state.CurrentVersion {
		phase, message := "RUNNING", ""
		if target.Generation > 0 && target.Generation == l.state.FailedGeneration {
			phase, message = l.state.LastPhase, l.state.LastError
			if phase == "" {
				phase = "FAILED"
			}
		}
		err := l.report(ctx, phase, target.TargetVersion, target.Generation, message)
		if errors.Is(err, ErrUnauthorized) {
			return err
		}
		return nil
	}
	if target.Generation <= 0 {
		return errors.New("invalid program target generation")
	}
	if err := l.report(ctx, "STAGING", target.TargetVersion, target.Generation, ""); err != nil {
		if errors.Is(err, ErrUnauthorized) {
			return err
		}
		return nil
	}
	path, hash, err := l.stage(ctx, target.TargetVersion)
	if err != nil {
		l.state.FailedGeneration, l.state.LastError, l.state.LastPhase = target.Generation, err.Error(), "FAILED"
		if err := l.save(); err != nil {
			return err
		}
		err = l.report(ctx, "FAILED", target.TargetVersion, target.Generation, err.Error())
		if errors.Is(err, ErrUnauthorized) {
			return err
		}
		return nil
	}
	// A changed/cancelled target or revoked credential must not stop a working
	// process just because an earlier download completed.
	var latest model.EdgeProgram
	if err = l.request(ctx, "GET", nil, &latest); err != nil {
		if errors.Is(err, ErrUnauthorized) {
			return err
		}
		return nil
	}
	if latest.Generation != target.Generation || latest.TargetVersion != target.TargetVersion {
		return nil
	}
	old := l.state
	// Persist the attempt before stopping the old process. A power loss at any
	// point before the successful final journal write restarts the old program
	// and requires an explicit new generation to retry the interrupted update.
	l.state.FailedGeneration, l.state.LastPhase, l.state.LastError = target.Generation, "ROLLED_BACK", "program switch was interrupted before verified completion"
	if err = l.save(); err != nil {
		return err
	}
	if err = l.report(ctx, "UPDATING", target.TargetVersion, target.Generation, ""); err != nil {
		l.state = old
		if saveErr := l.save(); saveErr != nil {
			return saveErr
		}
		if errors.Is(err, ErrUnauthorized) {
			return err
		}
		return nil
	}
	l.child.stop()
	child, version, err := l.start(ctx, path, target.TargetVersion)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		l.state.LastError = err.Error()
		if err = l.request(ctx, "GET", nil, &latest); err != nil {
			return err
		}
		// Current still points to the previously healthy program.
		if err = l.startCurrent(ctx); err != nil {
			return fmt.Errorf("rollback startup failed: %w", err)
		}
		err = l.report(ctx, "ROLLED_BACK", target.TargetVersion, target.Generation, l.state.LastError)
		if errors.Is(err, ErrUnauthorized) {
			return err
		}
		return nil
	}
	l.child = child
	l.state.PreviousPath, l.state.PreviousVersion, l.state.PreviousSHA256 = old.CurrentPath, old.CurrentVersion, old.CurrentSHA256
	l.state.CurrentPath, l.state.CurrentVersion, l.state.CurrentSHA256 = path, version, hash
	l.state.AppliedGeneration, l.state.FailedGeneration = target.Generation, 0
	l.state.LastPhase, l.state.LastError = "RUNNING", ""
	if err = l.save(); err != nil {
		return err
	}
	_ = l.prune()
	err = l.report(ctx, "RUNNING", "", target.Generation, "")
	if errors.Is(err, ErrUnauthorized) {
		return err
	}
	return nil
}

func (l *Launcher) stage(ctx context.Context, version string) (string, string, error) {
	if l.options.CatalogPolicy == "" {
		return "", "", errors.New("local trusted program catalog is not configured")
	}
	client, err := protocolcatalog.ReadPolicy(l.options.CatalogPolicy)
	if err != nil {
		return "", "", err
	}
	defer client.Close()
	catalog, err := client.Fetch(ctx)
	if err != nil {
		return "", "", err
	}
	for _, entry := range catalog.Entries {
		if entry.Kind != "edge-agent" || entry.ID != "iot-edge-agent" || entry.Version != version || entry.Platform != runtime.GOOS+"/"+runtime.GOARCH {
			continue
		}
		data, err := client.Source(ctx, entry)
		if err != nil {
			return "", "", err
		}
		if err := l.prune(); err != nil {
			return "", "", err
		}
		path := filepath.Join(l.options.DataDir, "agent-"+entry.SHA256)
		if runtime.GOOS == "windows" {
			path += ".exe"
		}
		// Do not replace an executable that may currently be running (Windows).
		if hash, err := binaryHash(path); err == nil && hash == entry.SHA256 {
			return path, hash, nil
		}
		if err = edgeagent.WriteState(path, data); err != nil {
			return "", "", err
		}
		if err = os.Chmod(path, 0700); err != nil {
			return "", "", err
		}
		return path, entry.SHA256, nil
	}
	return "", "", errors.New("signed catalog does not contain the requested native edge program")
}

// Only launcher-owned hash-named binaries are reclaimed. At most the current,
// previous and one staged binary are retained (3 x 32 MiB); bootstrap is local.
func (l *Launcher) prune() error {
	entries, err := os.ReadDir(l.options.DataDir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		name := entry.Name()
		hash := strings.TrimSuffix(strings.TrimPrefix(name, "agent-"), ".exe")
		decoded, err := hex.DecodeString(hash)
		if !strings.HasPrefix(name, "agent-") || len(decoded) != 32 || err != nil || entry.IsDir() {
			continue
		}
		path := filepath.Join(l.options.DataDir, name)
		if path != l.state.CurrentPath && path != l.state.PreviousPath && path != l.options.BootstrapBinary {
			if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
				return errors.New("cannot reclaim old program cache; upgrade stopped")
			}
		}
	}
	return nil
}
