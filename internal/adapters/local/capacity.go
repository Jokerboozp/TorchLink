package local

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"iot-platform/internal/adapters/rawstore"
	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

var _ ports.CapacityRawObjectCleaner = (*Archive)(nil)

func (a *Archive) DeleteCapacityRawObject(ctx context.Context, tenant string, q model.CapacityCleanupBatch, idx model.RawArchiveIndex) error {
	if err := rawstore.ValidateCapacityRawObjectScope(tenant, q, idx); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if filepath.Base(idx.ObjectBucket) != idx.ObjectBucket || idx.ObjectBucket == "." || idx.ObjectBucket == ".." || strings.ContainsAny(idx.ObjectBucket, `/\`+"\x00") || filepath.IsAbs(idx.ObjectKey) || strings.ContainsAny(idx.ObjectKey, `\`+"\x00") {
		return model.ErrResourceInUse
	}
	for _, part := range strings.Split(idx.ObjectKey, "/") {
		if part == "" || part == "." || part == ".." {
			return model.ErrResourceInUse
		}
	}
	base, err := filepath.Abs(a.root)
	if err != nil {
		return err
	}
	// Check the configured root and every ancestor too: opening a root must not
	// silently redirect through an existing link before object checks begin.
	current := filepath.VolumeName(base) + string(filepath.Separator)
	for _, part := range strings.Split(strings.TrimPrefix(base, current), string(filepath.Separator)) {
		if part == "" {
			continue
		}
		current = filepath.Join(current, part)
		st, err := os.Lstat(current)
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return err
		}
		if st.Mode()&os.ModeSymlink != 0 || !st.IsDir() {
			return model.ErrResourceInUse
		}
	}
	root, err := os.OpenRoot(base)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	defer root.Close()
	name := filepath.Join(idx.ObjectBucket, filepath.FromSlash(idx.ObjectKey))
	before, err := capacityLocalObjectInfo(root, name)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	file, err := root.Open(name)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil {
		return err
	}
	if !os.SameFile(before, opened) || !opened.Mode().IsRegular() {
		return model.ErrResourceInUse
	}
	if err = rawstore.ValidateCapacityRawObject(ctx, file, tenant, q, idx); err != nil {
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	// Recheck the complete path and the file identity before removal. Root
	// confines both operations even if a directory is concurrently renamed.
	after, err := capacityLocalObjectInfo(root, name)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !os.SameFile(opened, after) || opened.Size() != after.Size() || !opened.ModTime().Equal(after.ModTime()) {
		return model.ErrResourceInUse
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	err = root.Remove(name)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func capacityLocalObjectInfo(root *os.Root, name string) (os.FileInfo, error) {
	parts := strings.Split(name, string(filepath.Separator))
	for i := range parts {
		st, err := root.Lstat(filepath.Join(parts[:i+1]...))
		if err != nil {
			return nil, err
		}
		if st.Mode()&os.ModeSymlink != 0 || (i < len(parts)-1 && !st.IsDir()) || (i == len(parts)-1 && !st.Mode().IsRegular()) {
			return nil, model.ErrResourceInUse
		}
		if i == len(parts)-1 {
			return st, nil
		}
	}
	return nil, errors.New("invalid raw object path")
}
