package remote

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"
	"sync"
	"time"
)

// =========================================================================
// Google Drive Client Implementation (Drive API v3, OAuth2 refresh_token)
// =========================================================================
//
// Config fields:
//   client_id / client_secret / refresh_token — OAuth2 credentials
//     (obtainable via "rclone config" for a drive remote).
//   root_folder_name — target Drive folder name (created if missing),
//     default "CLICD-Backups". A literal folder ID may be given as
//     "id:<folderId>".
//
// Drive has no path semantics, so every operation resolves the path to a
// file/folder ID first (find-or-create for folders, cached per client).
// Large files use resumable uploads in 8 MiB chunks.

type GoogleDriveClient struct {
	ClientID       string
	ClientSecret   string
	RefreshToken   string
	RootFolderName string
	HTTP           *http.Client

	mu          sync.Mutex
	accessToken string
	tokenExpiry time.Time

	folderMu   sync.Mutex
	folderIDs  map[string]string // rel dir path -> folder ID
	baseIDOnce sync.Once
	baseID     string
	baseErr    error
}

func NewGoogleDriveClient(cfg map[string]string) (*GoogleDriveClient, error) {
	clientID := strings.TrimSpace(cfg["client_id"])
	clientSecret := strings.TrimSpace(cfg["client_secret"])
	refreshToken := strings.TrimSpace(cfg["refresh_token"])
	if clientID == "" || clientSecret == "" || refreshToken == "" {
		return nil, fmt.Errorf("Google Drive requires client_id, client_secret and refresh_token (see the hint below the form, e.g. via rclone config)")
	}
	root := strings.TrimSpace(cfg["root_folder_name"])
	if root == "" {
		root = "CLICD-Backups"
	}
	return &GoogleDriveClient{
		ClientID:       clientID,
		ClientSecret:   clientSecret,
		RefreshToken:   refreshToken,
		RootFolderName: root,
		HTTP:           &http.Client{},
		folderIDs:      map[string]string{},
	}, nil
}

func (c *GoogleDriveClient) apiBase() string { return "https://www.googleapis.com/drive/v3" }

func (c *GoogleDriveClient) uploadBase() string {
	return "https://www.googleapis.com/upload/drive/v3"
}

func (c *GoogleDriveClient) getAccessToken(ctx context.Context, force bool) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !force && c.accessToken != "" && time.Now().Before(c.tokenExpiry.Add(-60*time.Second)) {
		return c.accessToken, nil
	}
	form := url.Values{}
	form.Set("client_id", c.ClientID)
	form.Set("client_secret", c.ClientSecret)
	form.Set("refresh_token", c.RefreshToken)
	form.Set("grant_type", "refresh_token")
	req, err := http.NewRequestWithContext(ctx, "POST", "https://oauth2.googleapis.com/token",
		strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return "", fmt.Errorf("token request failed: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("token refresh failed (HTTP %d): %s", resp.StatusCode, truncateForLog(body))
	}
	var token struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &token); err != nil || token.AccessToken == "" {
		return "", fmt.Errorf("token response parse failed: %w", err)
	}
	c.accessToken = token.AccessToken
	c.tokenExpiry = time.Now().Add(time.Duration(token.ExpiresIn) * time.Second)
	return c.accessToken, nil
}

func (c *GoogleDriveClient) doDrive(ctx context.Context, method, rawURL string, body io.Reader, contentType string) (*http.Response, error) {
	token, err := c.getAccessToken(ctx, false)
	if err != nil {
		return nil, err
	}
	resp, err := c.doDriveWithToken(ctx, method, rawURL, body, contentType, token)
	if err == nil && resp.StatusCode == http.StatusUnauthorized {
		io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
		resp.Body.Close()
		token, err = c.getAccessToken(ctx, true)
		if err != nil {
			return nil, err
		}
		resp, err = c.doDriveWithToken(ctx, method, rawURL, body, contentType, token)
	}
	return resp, err
}

func (c *GoogleDriveClient) doDriveWithToken(ctx context.Context, method, rawURL string, body io.Reader, contentType, token string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, rawURL, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	return c.HTTP.Do(req)
}

// driveQueryEscape escapes a name for a Drive q expression (single quotes).
func driveQueryEscape(name string) string {
	return strings.ReplaceAll(name, "'", "\\'")
}

// baseFolderID resolves the root folder for the pool (cached once).
func (c *GoogleDriveClient) baseFolderID(ctx context.Context) (string, error) {
	c.baseIDOnce.Do(func() {
		if strings.HasPrefix(c.RootFolderName, "id:") {
			c.baseID = strings.TrimPrefix(c.RootFolderName, "id:")
			return
		}
		id, err := c.findOrCreateFolder(ctx, "root", c.RootFolderName)
		c.baseID = id
		c.baseErr = err
	})
	return c.baseID, c.baseErr
}

// driveList runs a files.list query and returns the matching files.
func (c *GoogleDriveClient) driveList(ctx context.Context, query, fields string) ([]struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Size int64  `json:"size"`
}, error) {
	params := url.Values{}
	params.Set("q", query)
	params.Set("fields", fields)
	params.Set("pageSize", "10")
	params.Set("supportsAllDrives", "true")
	params.Set("includeItemsFromAllDrives", "true")
	resp, err := c.doDrive(ctx, "GET", c.apiBase()+"/files?"+params.Encode(), nil, "")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Drive files.list failed (HTTP %d): %s", resp.StatusCode, truncateForLog(body))
	}
	var result struct {
		Files []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
			Size int64  `json:"size"`
		} `json:"files"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("Drive files.list response parse failed: %w", err)
	}
	return result.Files, nil
}

// findOrCreateFolder resolves one folder by name under parentID, creating it
// when missing. Serialized: concurrent syncs must not create duplicates.
func (c *GoogleDriveClient) findOrCreateFolder(ctx context.Context, parentID, name string) (string, error) {
	c.folderMu.Lock()
	defer c.folderMu.Unlock()
	files, err := c.driveList(ctx,
		fmt.Sprintf("'%s' in parents and name = '%s' and mimeType = 'application/vnd.google-apps.folder' and trashed = false",
			parentID, driveQueryEscape(name)),
		"files(id,name)")
	if err != nil {
		return "", err
	}
	if len(files) > 0 {
		return files[0].ID, nil
	}
	payload := fmt.Sprintf(`{"name":%q,"mimeType":"application/vnd.google-apps.folder","parents":[%q]}`, name, parentID)
	resp, err := c.doDrive(ctx, "POST", c.apiBase()+"/files?fields=id", strings.NewReader(payload), "application/json")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return "", fmt.Errorf("Drive folder create %q failed (HTTP %d): %s", name, resp.StatusCode, truncateForLog(body))
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(body, &created); err != nil || created.ID == "" {
		return "", fmt.Errorf("Drive folder create response parse failed: %w", err)
	}
	return created.ID, nil
}

// resolveFolderID resolves a directory path (relative to the pool base) to a
// Drive folder ID, creating missing segments. Results are cached.
func (c *GoogleDriveClient) resolveFolderID(ctx context.Context, relDir string) (string, error) {
	relDir = strings.Trim(relDir, "/")
	base, err := c.baseFolderID(ctx)
	if err != nil {
		return "", err
	}
	if relDir == "" {
		return base, nil
	}
	c.folderMu.Lock()
	if id, ok := c.folderIDs[relDir]; ok {
		c.folderMu.Unlock()
		return id, nil
	}
	c.folderMu.Unlock()

	parent := base
	resolved := ""
	for _, seg := range strings.Split(relDir, "/") {
		if seg == "" {
			continue
		}
		id, err := c.findOrCreateFolder(ctx, parent, seg)
		if err != nil {
			return "", err
		}
		parent = id
		resolved = strings.Trim(resolved+"/"+seg, "/")
		c.folderMu.Lock()
		c.folderIDs[resolved] = id
		c.folderMu.Unlock()
	}
	return parent, nil
}

// resolveFileID resolves a file path to its Drive file ID.
func (c *GoogleDriveClient) resolveFileID(ctx context.Context, remotePath string) (string, error) {
	remotePath = strings.Trim(remotePath, "/")
	relDir := path.Dir(remotePath)
	name := path.Base(remotePath)
	folderID, err := c.resolveFolderID(ctx, relDir)
	if err != nil {
		return "", err
	}
	files, err := c.driveList(ctx,
		fmt.Sprintf("'%s' in parents and name = '%s' and trashed = false", folderID, driveQueryEscape(name)),
		"files(id,name,size)")
	if err != nil {
		return "", err
	}
	if len(files) == 0 {
		return "", fmt.Errorf("Drive file not found: %s", remotePath)
	}
	return files[0].ID, nil
}

func (c *GoogleDriveClient) TestConnection(ctx context.Context) error {
	if _, err := c.DescribeConnection(ctx); err != nil {
		return err
	}
	return nil
}

// DescribeConnection returns the account and storage quota summary.
func (c *GoogleDriveClient) DescribeConnection(ctx context.Context) (string, error) {
	resp, err := c.doDrive(ctx, "GET", c.apiBase()+"/about?fields=user,storageQuota", nil, "")
	if err != nil {
		return "", fmt.Errorf("Drive about request failed: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("Drive about failed (HTTP %d): %s", resp.StatusCode, truncateForLog(body))
	}
	var about struct {
		User struct {
			EmailAddress string `json:"emailAddress"`
		} `json:"user"`
		StorageQuota struct {
			Limit int64 `json:"limit"`
			Usage int64 `json:"usage"`
		} `json:"storageQuota"`
	}
	if err := json.Unmarshal(body, &about); err != nil {
		return "", fmt.Errorf("Drive about response parse failed: %w", err)
	}
	quota := fmt.Sprintf("已用 %s", formatBytesShort(about.StorageQuota.Usage))
	if about.StorageQuota.Limit > 0 {
		quota += fmt.Sprintf(" / 总量 %s", formatBytesShort(about.StorageQuota.Limit))
	}
	return fmt.Sprintf("Google Drive 账号 %s，%s", about.User.EmailAddress, quota), nil
}

func (c *GoogleDriveClient) UploadFile(ctx context.Context, localPath, remotePath string) error {
	f, err := os.Open(localPath)
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	remotePath = strings.Trim(remotePath, "/")
	folderID, err := c.resolveFolderID(ctx, path.Dir(remotePath))
	if err != nil {
		return err
	}
	return c.resumableUpload(ctx, f, info.Size(), path.Base(remotePath), folderID)
}

func (c *GoogleDriveClient) resumableUpload(ctx context.Context, f *os.File, total int64, name, folderID string) error {
	metadata := fmt.Sprintf(`{"name":%q,"parents":[%q]}`, name, folderID)
	sessionURL := ""
	// Step 1: initiate the resumable session.
	req, err := http.NewRequestWithContext(ctx, "POST",
		c.uploadBase()+"/files?uploadType=resumable&supportsAllDrives=true&fields=id",
		strings.NewReader(metadata))
	if err != nil {
		return err
	}
	token, err := c.getAccessToken(ctx, false)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json; charset=UTF-8")
	req.Header.Set("X-Upload-Content-Type", "application/octet-stream")
	req.Header.Set("X-Upload-Content-Length", fmt.Sprintf("%d", total))
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("Drive resumable init for %s failed (HTTP %d): %s", name, resp.StatusCode, truncateForLog(body))
	}
	sessionURL = resp.Header.Get("Location")
	if sessionURL == "" {
		return fmt.Errorf("Drive resumable init for %s returned no session URL", name)
	}

	// Step 2: stream the file in chunks. Session PUTs carry no auth header.
	const chunkSize = 8 * 1024 * 1024
	buf := make([]byte, chunkSize)
	var offset int64
	for offset < total {
		chunkEnd := offset + chunkSize
		if chunkEnd > total {
			chunkEnd = total
		}
		n, err := f.ReadAt(buf[:chunkEnd-offset], offset)
		if err != nil && err != io.EOF {
			return err
		}
		if err := c.putResumableChunk(ctx, sessionURL, buf[:n], offset, total); err != nil {
			return err
		}
		offset += int64(n)
	}
	return nil
}

func (c *GoogleDriveClient) putResumableChunk(ctx context.Context, sessionURL string, chunk []byte, offset, total int64) error {
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		contentRange := fmt.Sprintf("bytes %d-%d/%d", offset, offset+int64(len(chunk))-1, total)
		req, err := http.NewRequestWithContext(ctx, "PUT", sessionURL, strings.NewReader(string(chunk)))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Range", contentRange)
		req.ContentLength = int64(len(chunk))
		resp, err := c.HTTP.Do(req)
		if err != nil {
			lastErr = err
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Duration(attempt+1) * 3 * time.Second):
			}
			continue
		}
		io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
		resp.Body.Close()
		switch {
		case resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusCreated:
			return nil
		case resp.StatusCode == http.StatusPermanentRedirect: // 308 Resume Incomplete
			// Chunk accepted; proceed with the next range.
			return nil
		default:
			lastErr = fmt.Errorf("Drive chunk upload failed (HTTP %d, range %s)", resp.StatusCode, contentRange)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Duration(attempt+1) * 3 * time.Second):
			}
		}
	}
	return lastErr
}

func (c *GoogleDriveClient) DownloadFile(ctx context.Context, remotePath, localPath string) error {
	fileID, err := c.resolveFileID(ctx, remotePath)
	if err != nil {
		return err
	}
	resp, err := c.doDrive(ctx, "GET", c.apiBase()+"/files/"+fileID+"?alt=media&supportsAllDrives=true", nil, "")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		return fmt.Errorf("Drive download %s failed (HTTP %d): %s", remotePath, resp.StatusCode, truncateForLog(body))
	}
	if err := os.MkdirAll(path.Dir(localPath), 0700); err != nil {
		return err
	}
	out, err := os.Create(localPath)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, resp.Body)
	return err
}

func (c *GoogleDriveClient) DeleteFile(ctx context.Context, remotePath string) error {
	fileID, err := c.resolveFileID(ctx, remotePath)
	if err != nil {
		// Deleting something that no longer exists is fine.
		if strings.Contains(err.Error(), "not found") {
			return nil
		}
		return err
	}
	resp, err := c.doDrive(ctx, "DELETE", c.apiBase()+"/files/"+fileID+"?supportsAllDrives=true", nil, "")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		return fmt.Errorf("Drive delete %s failed (HTTP %d)", remotePath, resp.StatusCode)
	}
	return nil
}

// DeleteDir removes a Drive folder (and everything inside it) in one call.
func (c *GoogleDriveClient) DeleteDir(ctx context.Context, remotePath string) error {
	folderID, err := c.resolveFolderID(ctx, remotePath)
	if err != nil {
		return err
	}
	resp, err := c.doDrive(ctx, "DELETE", c.apiBase()+"/files/"+folderID+"?supportsAllDrives=true", nil, "")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		return fmt.Errorf("Drive delete folder %s failed (HTTP %d)", remotePath, resp.StatusCode)
	}
	return nil
}
