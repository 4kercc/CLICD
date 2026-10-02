package remote

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

// Long transfers over a WAN die for ordinary reasons: a NAT or firewall idle
// timeout, a remote sshd keepalive limit, a flaky link. A snapshot upload is
// tens of gigabytes, so the transfer has to survive that — keepalives stop the
// session from being treated as idle, and every retry resumes at the byte
// offset the remote already holds instead of starting over from zero.
const (
	sftpTransferAttempts = 4
	sftpKeepaliveEvery   = 20 * time.Second
	// sftpPrefixCheckBytes is how much of a file is compared when deciding
	// whether a size match can be trusted.
	sftpPrefixCheckBytes = 64 << 10
	// sftpFreshUploadMax: files this small are rewritten every time. Resending a
	// few kilobytes is cheaper than reasoning about whether an old copy is
	// stale — regenerated metadata (snapshot-meta.json) can keep its size while
	// its content changes.
	sftpFreshUploadMax = 1 << 20
)

func sftpRetryDelay(attempt int) time.Duration {
	switch attempt {
	case 1:
		return 5 * time.Second
	case 2:
		return 15 * time.Second
	default:
		return 45 * time.Second
	}
}

func sleepWithContext(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// dial opens an SSH connection with TCP keepalive enabled and a periodic
// SSH-level keepalive request, so a long transfer is not dropped as an idle
// session by anything between here and the remote.
func (c *SFTPClient) dial() (*ssh.Client, error) {
	cfg, err := c.sshConfig()
	if err != nil {
		return nil, err
	}
	addr := fmt.Sprintf("%s:%d", c.Host, c.Port)
	dialer := &net.Dialer{Timeout: 15 * time.Second, KeepAlive: 30 * time.Second}
	conn, err := dialer.Dial("tcp", addr)
	if err != nil {
		return nil, err
	}
	sshConn, chans, reqs, err := ssh.NewClientConn(conn, addr, cfg)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	client := ssh.NewClient(sshConn, chans, reqs)
	go keepSSHAlive(client)
	return client, nil
}

func keepSSHAlive(client *ssh.Client) {
	ticker := time.NewTicker(sftpKeepaliveEvery)
	defer ticker.Stop()
	for range ticker.C {
		if _, _, err := client.SendRequest("keepalive@openssh.com", true, nil); err != nil {
			return
		}
	}
}

// remoteFileSize returns the size of a remote file, or -1 when it cannot be
// read (missing file, unusable stat, dead session).
func (c *SFTPClient) remoteFileSize(client *ssh.Client, fullRemote string) int64 {
	session, err := client.NewSession()
	if err != nil {
		return -1
	}
	defer session.Close()
	quoted := shellQuote(fullRemote)
	command := fmt.Sprintf("stat -c %%s %s 2>/dev/null || stat -f %%z %s 2>/dev/null", quoted, quoted)
	out, err := session.Output(command)
	if err != nil {
		return -1
	}
	size, convErr := strconv.ParseInt(strings.TrimSpace(string(out)), 10, 64)
	if convErr != nil {
		return -1
	}
	return size
}

func runRemote(client *ssh.Client, command string) error {
	session, err := client.NewSession()
	if err != nil {
		return err
	}
	defer session.Close()
	return session.Run(command)
}

// UploadFile sends a file to the remote, resuming and retrying when a transfer
// is interrupted.
//
// A file that is already complete is left alone, which makes re-running a
// snapshot sync after a failure cheap: only the files that never finished are
// sent again, and each of those continues from its own byte offset.
func (c *SFTPClient) UploadFile(ctx context.Context, localPath, remotePath string) error {
	srcFile, err := os.Open(localPath)
	if err != nil {
		return err
	}
	defer srcFile.Close()
	stat, err := srcFile.Stat()
	if err != nil {
		return err
	}
	fullRemote := path.Join(c.BasePath, remotePath)

	var lastErr error
	for attempt := 1; attempt <= sftpTransferAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		if attempt > 1 {
			if err := sleepWithContext(ctx, sftpRetryDelay(attempt-1)); err != nil {
				return err
			}
		}
		client, err := c.dial()
		if err != nil {
			lastErr = err
			continue
		}
		err = c.uploadOnce(ctx, client, srcFile, stat.Size(), fullRemote)
		client.Close()
		if err == nil {
			return nil
		}
		lastErr = err
		if ctx.Err() != nil {
			return ctx.Err()
		}
		fmt.Printf("Warning: upload of %s interrupted (%v); retrying\n", remotePath, err)
	}
	return fmt.Errorf("upload %s failed after %d attempts: %w", remotePath, sftpTransferAttempts, lastErr)
}

// remotePrefixMatches reports whether the first n bytes of the remote file are
// identical to the first n bytes of the local file.
//
// This is the guard that lets a resume — or a skip — trust a size match. A byte
// count alone cannot tell a file from a different file of the same length, and
// appending onto the wrong prefix silently produces a corrupt remote copy.
func (c *SFTPClient) remotePrefixMatches(client *ssh.Client, src *os.File, n int64, fullRemote string) bool {
	if n > sftpPrefixCheckBytes {
		n = sftpPrefixCheckBytes
	}
	if n <= 0 {
		return true
	}
	buf := make([]byte, n)
	if _, err := src.ReadAt(buf, 0); err != nil {
		return false
	}
	localSum := md5.Sum(buf)

	session, err := client.NewSession()
	if err != nil {
		return false
	}
	defer session.Close()
	command := fmt.Sprintf("head -c %d %s | md5sum", n, shellQuote(fullRemote))
	out, err := session.Output(command)
	if err != nil {
		return false
	}
	return strings.HasPrefix(strings.TrimSpace(string(out)), hex.EncodeToString(localSum[:]))
}

func (c *SFTPClient) uploadOnce(ctx context.Context, client *ssh.Client, src *os.File, size int64, fullRemote string) error {
	offset := c.remoteFileSize(client, fullRemote)
	if offset < 0 {
		offset = 0
	}
	switch {
	case size <= sftpFreshUploadMax:
		offset = 0 // small files are always rewritten
	case offset > 0 && !c.remotePrefixMatches(client, src, offset, fullRemote):
		offset = 0 // what is up there is not this file: replace it wholesale
	}
	if offset >= size {
		return nil // already there, and verified to be this file
	}
	if err := runRemote(client, "mkdir -p "+shellQuote(path.Dir(fullRemote))); err != nil {
		return err
	}
	if _, err := src.Seek(offset, io.SeekStart); err != nil {
		return err
	}

	session, err := client.NewSession()
	if err != nil {
		return err
	}
	defer session.Close()

	// Appending assumes the remote file is a byte prefix of the local one, which
	// is exactly what an interrupted `cat` leaves behind.
	command := "cat >> " + shellQuote(fullRemote)
	if offset == 0 {
		command = "cat > " + shellQuote(fullRemote)
	}
	stdin, err := session.StdinPipe()
	if err != nil {
		return err
	}
	if err := session.Start(command); err != nil {
		return err
	}
	if _, err := io.Copy(stdin, src); err != nil {
		stdin.Close()
		return err
	}
	stdin.Close()
	if err := session.Wait(); err != nil {
		return err
	}

	if final := c.remoteFileSize(client, fullRemote); final != size {
		return fmt.Errorf("remote size %d does not match local size %d", final, size)
	}
	return nil
}

// DownloadFile pulls a file back, resuming into a partial local file and
// retrying on failure, mirroring UploadFile.
func (c *SFTPClient) DownloadFile(ctx context.Context, remotePath, localPath string) error {
	fullRemote := path.Join(c.BasePath, remotePath)
	if err := os.MkdirAll(filepath.Dir(localPath), 0o755); err != nil {
		return err
	}

	var lastErr error
	for attempt := 1; attempt <= sftpTransferAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		if attempt > 1 {
			if err := sleepWithContext(ctx, sftpRetryDelay(attempt-1)); err != nil {
				return err
			}
		}
		client, err := c.dial()
		if err != nil {
			lastErr = err
			continue
		}
		err = c.downloadOnce(client, fullRemote, localPath)
		client.Close()
		if err == nil {
			return nil
		}
		lastErr = err
		if ctx.Err() != nil {
			return ctx.Err()
		}
		fmt.Printf("Warning: download of %s interrupted (%v); retrying\n", remotePath, err)
	}
	return fmt.Errorf("download %s failed after %d attempts: %w", remotePath, sftpTransferAttempts, lastErr)
}

func (c *SFTPClient) downloadOnce(client *ssh.Client, fullRemote, localPath string) error {
	remoteSize := c.remoteFileSize(client, fullRemote)
	if remoteSize < 0 {
		return fmt.Errorf("remote file %s not found", fullRemote)
	}
	offset := int64(0)
	if st, err := os.Stat(localPath); err == nil {
		offset = st.Size()
		if offset > remoteSize {
			offset = 0 // local is longer than the remote original: start over
		}
	}
	if offset == remoteSize && offset > 0 {
		return nil // already complete
	}

	session, err := client.NewSession()
	if err != nil {
		return err
	}
	defer session.Close()

	command := "cat " + shellQuote(fullRemote)
	if offset > 0 {
		command = fmt.Sprintf("tail -c +%d %s", offset+1, shellQuote(fullRemote))
	}
	stdout, err := session.StdoutPipe()
	if err != nil {
		return err
	}
	flags := os.O_CREATE | os.O_WRONLY | os.O_TRUNC
	if offset > 0 {
		flags = os.O_CREATE | os.O_WRONLY | os.O_APPEND
	}
	dst, err := os.OpenFile(localPath, flags, 0o644)
	if err != nil {
		return err
	}
	if err := session.Start(command); err != nil {
		dst.Close()
		return err
	}
	_, copyErr := io.Copy(dst, stdout)
	dst.Close()
	waitErr := session.Wait()
	if copyErr != nil {
		return copyErr
	}
	if waitErr != nil {
		return waitErr
	}
	st, err := os.Stat(localPath)
	if err != nil {
		return err
	}
	if st.Size() != remoteSize {
		return fmt.Errorf("local size %d does not match remote size %d", st.Size(), remoteSize)
	}
	return nil
}
