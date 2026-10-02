package remote

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"

	"golang.org/x/crypto/ssh"
)

// =========================================================================
// Recursive deletion (DeletePath / PruneEmptyDir) per backend
// =========================================================================

// DeletePath removes the remote directory tree for a snapshot or backup. The
// recorded remote path is a directory, and on SFTP the cheapest reliable way
// to remove one is to let the remote shell do it.
func (c *SFTPClient) DeletePath(ctx context.Context, remotePath string) (bool, error) {
	return c.removeSFTPPath(ctx, remotePath, true)
}

func (c *SFTPClient) PruneEmptyDir(ctx context.Context, remotePath string) error {
	_, err := c.removeSFTPPath(ctx, remotePath, false)
	return err
}

// removeSFTPPath removes one path over a single command: a Go ssh session runs
// exactly one command, so existence and removal are combined instead of probed
// separately. Exit status 7 means "was not there", which the caller reports as
// absent rather than removed.
func (c *SFTPClient) removeSFTPPath(ctx context.Context, remotePath string, recursive bool) (bool, error) {
	client, err := c.dial()
	if err != nil {
		return false, err
	}
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		return false, err
	}
	defer session.Close()

	fullRemote := path.Join(c.BasePath, strings.TrimLeft(remotePath, "/"))
	quoted := shellQuote(fullRemote)
	action := "rm -rf "
	if !recursive {
		action = "rmdir "
	}
	command := fmt.Sprintf("if [ -e %s ]; then %s%s; else exit 7; fi", quoted, action, quoted)
	out, err := session.CombinedOutput(command)
	if err != nil {
		var exitErr *ssh.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitStatus() == 7 {
			return false, nil
		}
		return false, fmt.Errorf("remove %s: %w: %s", fullRemote, err, strings.TrimSpace(string(out)))
	}
	return true, nil
}

// DeletePath on WebDAV deletes a collection, which RFC 4918 defines as
// recursive, so a snapshot directory and its contents go in one request.
func (w *WebDAVClient) DeletePath(ctx context.Context, remotePath string) (bool, error) {
	fullURL := w.URL + "/" + strings.TrimLeft(strings.Trim(remotePath, "/"), "/")
	req, err := http.NewRequestWithContext(ctx, "DELETE", fullURL, nil)
	if err != nil {
		return false, err
	}
	w.setAuth(req)
	resp, err := w.HTTP.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	switch {
	case resp.StatusCode == http.StatusNotFound:
		return false, nil
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		return true, nil
	default:
		return false, fmt.Errorf("delete %s: unexpected status %d", fullURL, resp.StatusCode)
	}
}

// PruneEmptyDir removes a WebDAV collection only if it is empty: a server that
// refuses to delete a non-empty collection answers 4xx, which is ignored.
func (w *WebDAVClient) PruneEmptyDir(ctx context.Context, remotePath string) error {
	_, err := w.DeletePath(ctx, remotePath)
	return err
}

// DeletePath on object storage walks the prefix, because a directory there is
// only a naming convention shared by the objects underneath it.
func (m *MinIOClient) DeletePath(ctx context.Context, remotePath string) (bool, error) {
	prefix := strings.Trim(strings.TrimLeft(remotePath, "/"), "/")
	if prefix == "" {
		return false, fmt.Errorf("refusing to delete the whole bucket")
	}
	keys, err := m.listPrefix(ctx, prefix+"/")
	if err != nil {
		return false, err
	}
	// The exact key may be an object rather than a prefix.
	keys = append(keys, prefix)

	removed := false
	for _, key := range keys {
		existed, err := m.deleteObject(ctx, key)
		if err != nil {
			return removed, err
		}
		if existed {
			removed = true
		}
	}
	return removed, nil
}

type minioListResult struct {
	IsTruncated           bool   `xml:"IsTruncated"`
	NextContinuationToken string `xml:"NextContinuationToken"`
	Contents              []struct {
		Key string `xml:"Key"`
	} `xml:"Contents"`
}

// listPrefix returns every object key under a prefix, following continuation
// tokens. The page count is capped so a misconfigured prefix cannot spin.
func (m *MinIOClient) listPrefix(ctx context.Context, prefix string) ([]string, error) {
	scheme := "https"
	if !m.UseSSL {
		scheme = "http"
	}
	keys := make([]string, 0, 16)
	token := ""
	for page := 0; page < 1000; page++ {
		query := url.Values{}
		query.Set("list-type", "2")
		query.Set("prefix", prefix)
		if token != "" {
			query.Set("continuation-token", token)
		}
		urlStr := fmt.Sprintf("%s://%s/%s?%s", scheme, m.Endpoint, m.Bucket, query.Encode())

		req, err := http.NewRequestWithContext(ctx, "GET", urlStr, nil)
		if err != nil {
			return nil, err
		}
		req.Host = m.Endpoint
		// Signed query parameters must be the same set that was signed.
		m.signS3V4(req, "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855")

		resp, err := m.HTTP.Do(req)
		if err != nil {
			return nil, err
		}
		body, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			return nil, readErr
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return nil, fmt.Errorf("list %s: unexpected status %d: %s", prefix, resp.StatusCode, strings.TrimSpace(string(body)))
		}
		var parsed minioListResult
		if err := xml.Unmarshal(body, &parsed); err != nil {
			return nil, fmt.Errorf("list %s: %w", prefix, err)
		}
		for _, item := range parsed.Contents {
			if strings.TrimSpace(item.Key) != "" {
				keys = append(keys, item.Key)
			}
		}
		if !parsed.IsTruncated || parsed.NextContinuationToken == "" {
			return keys, nil
		}
		token = parsed.NextContinuationToken
	}
	return nil, fmt.Errorf("list %s: too many pages", prefix)
}

// deleteObject removes one key and reports whether it existed.
func (m *MinIOClient) deleteObject(ctx context.Context, key string) (bool, error) {
	scheme := "https"
	if !m.UseSSL {
		scheme = "http"
	}
	urlStr := fmt.Sprintf("%s://%s/%s/%s", scheme, m.Endpoint, m.Bucket, strings.TrimLeft(key, "/"))
	req, err := http.NewRequestWithContext(ctx, "DELETE", urlStr, nil)
	if err != nil {
		return false, err
	}
	req.Host = m.Endpoint
	m.signS3V4(req, "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855")

	resp, err := m.HTTP.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	switch {
	case resp.StatusCode == http.StatusNotFound:
		return false, nil
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		return true, nil
	default:
		return false, fmt.Errorf("delete %s: unexpected status %d", key, resp.StatusCode)
	}
}
