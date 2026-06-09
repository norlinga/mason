package remote

import (
	"context"
	"encoding/base64"
	"fmt"
	"path"
	"strings"
)

// RemoteFS performs filesystem operations on a remote host.
type RemoteFS interface {
	MkdirAll(ctx context.Context, remotePath string) error
	Symlink(ctx context.Context, linkPath, target string) error
	Remove(ctx context.Context, remotePath string) error
	ReadFile(ctx context.Context, remotePath string) ([]byte, error)
	WriteFile(ctx context.Context, remotePath string, data []byte, mode FileMode) error
}

// FileMode is a remote file permission bitmask (e.g. 0o644).
type FileMode uint32

const defaultFileMode FileMode = 0o644

// SSHFS implements RemoteFS over SSH shell commands.
type SSHFS struct {
	runner CommandRunner
}

// NewSSHFS creates a remote filesystem backed by SSH commands.
func NewSSHFS(runner CommandRunner) *SSHFS {
	return &SSHFS{runner: runner}
}

// MkdirAll creates a remote directory and any missing parents.
func (f *SSHFS) MkdirAll(ctx context.Context, remotePath string) error {
	remotePath = strings.TrimSpace(remotePath)
	if remotePath == "" {
		return fmt.Errorf("remote path is required")
	}

	_, err := f.runner.Run(ctx, fmt.Sprintf("mkdir -p %s", shellQuote(remotePath)))
	return err
}

// Symlink creates or replaces a symbolic link at linkPath pointing to target.
func (f *SSHFS) Symlink(ctx context.Context, linkPath, target string) error {
	linkPath = strings.TrimSpace(linkPath)
	target = strings.TrimSpace(target)
	if linkPath == "" {
		return fmt.Errorf("symlink path is required")
	}
	if target == "" {
		return fmt.Errorf("symlink target is required")
	}

	_, err := f.runner.Run(ctx, fmt.Sprintf("ln -sfn %s %s", shellQuote(target), shellQuote(linkPath)))
	return err
}

// Remove deletes a remote file or symlink.
func (f *SSHFS) Remove(ctx context.Context, remotePath string) error {
	remotePath = strings.TrimSpace(remotePath)
	if remotePath == "" {
		return fmt.Errorf("remote path is required")
	}

	_, err := f.runner.Run(ctx, fmt.Sprintf("rm -f %s", shellQuote(remotePath)))
	return err
}

// ReadFile returns the contents of a remote file.
func (f *SSHFS) ReadFile(ctx context.Context, remotePath string) ([]byte, error) {
	remotePath = strings.TrimSpace(remotePath)
	if remotePath == "" {
		return nil, fmt.Errorf("remote path is required")
	}

	result, err := f.runner.Run(ctx, fmt.Sprintf("cat %s", shellQuote(remotePath)))
	if err != nil {
		return nil, err
	}
	return []byte(result.Stdout), nil
}

// WriteFile writes data to a remote file, creating parent directories as needed.
func (f *SSHFS) WriteFile(ctx context.Context, remotePath string, data []byte, mode FileMode) error {
	remotePath = strings.TrimSpace(remotePath)
	if remotePath == "" {
		return fmt.Errorf("remote path is required")
	}
	if mode == 0 {
		mode = defaultFileMode
	}

	dir := path.Dir(remotePath)
	if dir != "." && dir != "/" {
		if err := f.MkdirAll(ctx, dir); err != nil {
			return err
		}
	}

	encoded := base64.StdEncoding.EncodeToString(data)
	command := fmt.Sprintf(
		"printf %%s %s | base64 -d > %s && chmod %s %s",
		shellQuote(encoded),
		shellQuote(remotePath),
		formatFileMode(mode),
		shellQuote(remotePath),
	)
	_, err := f.runner.Run(ctx, command)
	return err
}

// RemotePath joins a target root with a relative remote path.
func RemotePath(root, rel string) string {
	root = strings.TrimSpace(root)
	rel = strings.TrimLeft(strings.TrimSpace(rel), "/")
	if root == "" {
		return rel
	}
	root = strings.TrimRight(root, "/")
	if root == "" {
		root = "/"
	}
	if rel == "" {
		return root
	}
	if root == "/" {
		return root + rel
	}
	return root + "/" + rel
}

func shellQuote(value string) string {
	if value == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func formatFileMode(mode FileMode) string {
	return fmt.Sprintf("%o", mode&0o777)
}
