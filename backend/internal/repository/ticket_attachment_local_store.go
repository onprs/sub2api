package repository

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/google/uuid"
)

type ticketAttachmentLocalStore struct {
	root string
}

func newTicketAttachmentLocalStore(cfg *config.Config) (*ticketAttachmentLocalStore, error) {
	root := "./data/ticket-attachments"
	if cfg != nil && strings.TrimSpace(cfg.Ticketing.LocalStorageRoot) != "" {
		root = strings.TrimSpace(cfg.Ticketing.LocalStorageRoot)
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve ticket attachment root: %w", err)
	}
	if err := os.MkdirAll(absolute, 0o700); err != nil {
		return nil, fmt.Errorf("create ticket attachment root: %w", err)
	}
	if err := os.Chmod(absolute, 0o700); err != nil {
		return nil, fmt.Errorf("secure ticket attachment root: %w", err)
	}
	info, err := os.Lstat(absolute)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return nil, fmt.Errorf("ticket attachment root must be a real directory")
	}
	evaluated, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return nil, fmt.Errorf("evaluate ticket attachment root: %w", err)
	}
	if !sameFilesystemPath(evaluated, absolute) {
		return nil, fmt.Errorf("ticket attachment root must not traverse symbolic links")
	}
	return &ticketAttachmentLocalStore{root: filepath.Clean(absolute)}, nil
}

func (s *ticketAttachmentLocalStore) Provider() string { return "local" }

func (s *ticketAttachmentLocalStore) Put(_ context.Context, objectKey string, data []byte, _ string) error {
	path, err := s.resolve(objectKey)
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return err
	}
	if err := s.rejectSymlinkPath(dir); err != nil {
		return err
	}
	if _, err := os.Lstat(path); err == nil {
		return fmt.Errorf("ticket attachment object already exists")
	} else if !os.IsNotExist(err) {
		return err
	}

	tempPath := filepath.Join(dir, "."+filepath.Base(path)+"."+uuid.NewString()+".tmp")
	file, err := os.OpenFile(tempPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	cleanup := true
	defer func() {
		_ = file.Close()
		if cleanup {
			_ = os.Remove(tempPath)
		}
	}()
	if err := file.Chmod(0o600); err != nil {
		return err
	}
	if _, err := io.Copy(file, bytes.NewReader(data)); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := os.Rename(tempPath, path); err != nil {
		return err
	}
	cleanup = false
	if runtime.GOOS != "windows" {
		directory, err := os.Open(dir)
		if err != nil {
			return err
		}
		defer func() { _ = directory.Close() }()
		if err := directory.Sync(); err != nil {
			return err
		}
	}
	return nil
}

func (s *ticketAttachmentLocalStore) Open(_ context.Context, objectKey string) (io.ReadCloser, error) {
	path, err := s.resolve(objectKey)
	if err != nil {
		return nil, err
	}
	if err := s.rejectSymlinkPath(path); err != nil {
		return nil, err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("ticket attachment object is not a regular file")
	}
	return os.Open(path)
}

func (s *ticketAttachmentLocalStore) Delete(_ context.Context, objectKey string) error {
	path, err := s.resolve(objectKey)
	if err != nil {
		return err
	}
	if err := s.rejectSymlinkPath(path); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func (s *ticketAttachmentLocalStore) Probe(ctx context.Context) error {
	key := filepath.ToSlash(filepath.Join(".probe", uuid.NewString()))
	payload := []byte("ticket-attachment-storage-probe:" + uuid.NewString())
	if err := s.Put(ctx, key, payload, "text/plain"); err != nil {
		return err
	}
	defer func() { _ = s.Delete(context.Background(), key) }()
	body, err := s.Open(ctx, key)
	if err != nil {
		return err
	}
	read, err := io.ReadAll(body)
	closeErr := body.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if sha256.Sum256(read) != sha256.Sum256(payload) {
		return fmt.Errorf("ticket attachment local probe checksum mismatch")
	}
	return s.Delete(ctx, key)
}

func (s *ticketAttachmentLocalStore) resolve(objectKey string) (string, error) {
	if objectKey == "" || strings.HasPrefix(objectKey, "/") || strings.Contains(objectKey, `\`) || filepath.IsAbs(objectKey) {
		return "", fmt.Errorf("invalid ticket attachment object key")
	}
	cleanSlash := filepath.ToSlash(filepath.Clean(filepath.FromSlash(objectKey)))
	if cleanSlash == "." || cleanSlash != objectKey || strings.HasPrefix(cleanSlash, "../") {
		return "", fmt.Errorf("invalid ticket attachment object key")
	}
	path := filepath.Join(s.root, filepath.FromSlash(cleanSlash))
	relative, err := filepath.Rel(s.root, path)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("ticket attachment object escapes storage root")
	}
	return path, nil
}

func (s *ticketAttachmentLocalStore) rejectSymlinkPath(path string) error {
	relative, err := filepath.Rel(s.root, path)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return fmt.Errorf("ticket attachment path escapes storage root")
	}
	current := s.root
	if relative == "." {
		return nil
	}
	for _, part := range strings.Split(relative, string(filepath.Separator)) {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("ticket attachment path contains a symbolic link")
		}
	}
	return nil
}

func sameFilesystemPath(left, right string) bool {
	left = filepath.Clean(left)
	right = filepath.Clean(right)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(left, right)
	}
	return left == right
}
