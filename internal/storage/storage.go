package storage

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"

	"github.com/google/uuid"
)

const (
	StorageInternal = "internal"
	StorageExternal = "external"
)

type Storage struct {
	InternalRoot string
	ExternalRoot string
}

func New(internal, external string) *Storage {
	return &Storage{InternalRoot: internal, ExternalRoot: external}
}

func (s *Storage) resolveRoot(storageType string) (string, error) {
	switch storageType {
	case StorageInternal:
		return s.InternalRoot, nil
	case StorageExternal:
		return s.ExternalRoot, nil
	default:
		return "", errors.New("invalid storage type")
	}
}

// SaveStream saves a stream of bytes to a file under user folder and returns stored relative path
func (s *Storage) SaveStream(ctx context.Context, storageType, userID, relPath string, r io.Reader) (string, int64, error) {
	root, err := s.resolveRoot(storageType)
	if err != nil { return "",0,err }
	userDir := filepath.Join(root, userID, filepath.Dir(relPath))
	if err := os.MkdirAll(userDir, 0755); err != nil { return "",0,err }

	fileID := uuid.New().String()
	storedPath := filepath.Join(userDir, fileID)
	f, err := os.Create(storedPath)
	if err != nil { return "",0,err }
	defer f.Close()

	n, err := io.Copy(f, r)
	if err != nil { return "",0,err }
	return filepath.Join(userID, filepath.Dir(relPath), fileID), n, nil
}

func (s *Storage) OpenForRead(ctx context.Context, storageType, relStoredPath string) (*os.File, error) {
	root, err := s.resolveRoot(storageType)
	if err != nil { return nil, err }
	abs := filepath.Join(root, relStoredPath)
	return os.Open(abs)
}