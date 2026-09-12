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
// OneDrive Client Implementation (Microsoft Graph, OAuth2 refresh_token)
// =========================================================================
//
// Config fields:
//   client_id / client_secret / refresh_token — OAuth2 credentials
//     (obtainable via "rclone config" for an onedrive remote).
//   tenant    — Azure AD tenant id, defaults to "common".
//   root_path — remote base folder, defaults to "/clicd-backups".
//
// Large files are uploaded through a Graph upload session in 8 MiB chunks,
// so GB-sized backups work without any request-body timeout pressure.

type OneDriveClient struct {
	ClientID     string
	ClientSecret string
	RefreshToken string
	Tenant       string
	RootPath     string
	HTTP         *http.Client

	mu          sync.Mutex
	accessToken string
	tokenExpiry time.Time
	knownDirs   map[string]bool
}

func NewOneDriveClient(cfg map[string]string) (*OneDriveClient, error) {
	clientID := strings.TrimSpace(cfg["client_id"])
	clientSecret := strings.TrimSpace(cfg["client_secret"])
	refreshToken := strings.TrimSpace(cfg["refresh_token"])
	if clientID == "" || clientSecret == "" || refreshToken == "" {
		return nil, fmt.Errorf("OneDrive requires client_id, client_secret and refresh_token (see the hint below the form, e.g. via rclone config)")
	}
	tenant := strings.TrimSpace(cfg["tenant"])
	if tenant == "" {
		tenant = "common"
	}
	rootPath := strings.TrimSpace(cfg["root_path"])
	if rootPath == "" {
		rootPath = "/clicd-backups"
	}
	return &OneDriveClient{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		RefreshToken: refreshToken,
		Tenant:       tenant,
		RootPath:     rootPath,
		HTTP:         &http.Client{},
		knownDirs:    map[string]bool{},
	}, nil
}

func (c *OneDriveClient) graphAPI(path string) string {
	return "https://graph.microsoft.com/v1.0" + path
}

func (c *OneDriveClient) getAccessToken(ctx context.Context, force bool) (string, error) {
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
	form.Set("scope", "offline_access Files.ReadWrite.All")
	req, err := http.NewRequestWithContext(ctx, "POST",
		fmt.Sprintf("https://login.microsoftonline.com/%s/oauth2/v2.0/token", c.Tenant),
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
		AccessToken  string `json:"access_token"`
		ExpiresIn    int    `json:"expires_in"`
		RefreshToken string `json:"refresh_token"`
	}
	if err := json.Unmarshal(body, &token); err != nil || token.AccessToken == "" {
		return "", fmt.Errorf("token response parse failed: %w", err)
	}
	c.accessToken = token.AccessToken
	c.tokenExpiry = time.Now().Add(time.Duration(token.ExpiresIn) * time.Second)
	// Microsoft may roll the refresh token; keep the newest one for later refreshes.
	if token.RefreshToken != "" {
		c.RefreshToken = token.RefreshToken
	}
	return c.accessToken, nil
}

// doGraph performs an authenticated Graph request, refreshing the token once on 401.
func (c *OneDriveClient) doGraph(ctx context.Context, method, rawURL string, body io.Reader, contentType string) (*http.Response, error) {
	token, err := c.getAccessToken(ctx, false)
	if err != nil {
		return nil, err
	}
	resp, err := c.doGraphWithToken(ctx, method, rawURL, body, contentType, token)
	if err == nil && resp.StatusCode == http.StatusUnauthorized {
		io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
		resp.Body.Close()
		token, err = c.getAccessToken(ctx, true)
		if err != nil {
			return nil, err
		}
		resp, err = c.doGraphWithToken(ctx, method, rawURL, body, contentType, token)
	}
	return resp, err
}

func (c *OneDriveClient) doGraphWithToken(ctx context.Context, method, rawURL string, body io.Reader, contentType, token string) (*http.Response, error) {
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

// itemRef builds the Graph path-addressing reference ("root:/a/b") for a
// remote path relative to the pool base. Every segment is URL-escaped.
// Actions are appended with an extra colon (e.g. ref + ":/content").
func (c *OneDriveClient) itemRef(remoteRelPath string) string {
	joined := strings.Trim(c.RootPath, "/") + "/" + strings.Trim(remoteRelPath, "/")
	segments := strings.Split(joined, "/")
	escaped := make([]string, 0, len(segments))
	for _, seg := range segments {
		if seg == "" {
			continue
		}
		escaped = append(escaped, url.PathEscape(seg))
	}
	return "root:/" + strings.Join(escaped, "/")
}

// ensureOneDriveFolder creates every missing folder segment of the relDir
// (relative to the pool base) via Graph children posts. Results are cached.
func (c *OneDriveClient) ensureOneDriveFolder(ctx context.Context, relDir string) error {
	relDir = strings.Trim(relDir, "/")
	if relDir == "" {
		return nil
	}
	joined := strings.Trim(c.RootPath, "/") + "/" + relDir
	if c.knownDirs[joined] {
		return nil
	}
	segments := strings.Split(joined, "/")
	current := ""
	for _, seg := range segments {
		if seg == "" {
			continue
		}
		parentRef := "root"
		if current != "" {
			parentRef = "root:/" + strings.Join(escapeSegments(strings.Split(strings.Trim(current, "/"), "/")), "/")
		}
		current = current + "/" + seg
		if c.knownDirs[current] {
			continue
		}
		var childrenURL string
		if parentRef == "root" {
			childrenURL = c.graphAPI("/me/drive/root/children")
		} else {
			childrenURL = c.graphAPI("/me/drive/" + parentRef + ":/children")
		}
		// conflictBehavior=fail: an existing folder yields 409, which is fine.
		payload := fmt.Sprintf(`{"name":%q,"folder":{},"@microsoft.graph.conflictBehavior":"fail"}`, seg)
		resp, err := c.doGraph(ctx, "POST", childrenURL, strings.NewReader(payload), "application/json")
		if err != nil {
			return err
		}
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		resp.Body.Close()
		if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusConflict {
			return fmt.Errorf("create OneDrive folder %s failed (HTTP %d): %s", current, resp.StatusCode, truncateForLog(body))
		}
		c.knownDirs[current] = true
	}
	return nil
}

func escapeSegments(segments []string) []string {
	escaped := make([]string, 0, len(segments))
	for _, seg := range segments {
		if seg == "" {
			continue
		}
		escaped = append(escaped, url.PathEscape(seg))
	}
	return escaped
}

func (c *OneDriveClient) TestConnection(ctx context.Context) error {
	if _, err := c.DescribeConnection(ctx); err != nil {
		return err
	}
	return nil
}

// DescribeConnection returns the account name and storage quota summary.
func (c *OneDriveClient) DescribeConnection(ctx context.Context) (string, error) {
	resp, err := c.doGraph(ctx, "GET", c.graphAPI("/me/drive?$select=displayName,quota"), nil, "")
	if err != nil {
		return "", fmt.Errorf("Graph request failed: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("Graph /me/drive failed (HTTP %d): %s", resp.StatusCode, truncateForLog(body))
	}
	var drive struct {
		DisplayName string `json:"displayName"`
		Quota       struct {
			Total     int64 `json:"total"`
			Used      int64 `json:"used"`
			Remaining int64 `json:"remaining"`
		} `json:"quota"`
	}
	if err := json.Unmarshal(body, &drive); err != nil {
		return "", fmt.Errorf("Graph /me/drive response parse failed: %w", err)
	}
	return fmt.Sprintf("OneDrive 账号 %s，已用 %s / 总量 %s",
		drive.DisplayName,
		formatBytesShort(drive.Quota.Used),
		formatBytesShort(drive.Quota.Total)), nil
}

func (c *OneDriveClient) UploadFile(ctx context.Context, localPath, remotePath string) error {
	f, err := os.Open(localPath)
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}

	relDir := path.Dir(strings.Trim(remotePath, "/"))
	if err := c.ensureOneDriveFolder(ctx, relDir); err != nil {
		return err
	}

	item := c.itemRef(remotePath)
	total := info.Size()
	if total <= 4*1024*1024 {
		// Simple upload: PUT /me/drive/root:/path:/content (auto-replaces existing item).
		uploadURL := c.graphAPI("/me/drive/" + item + ":/content")
		resp, err := c.doGraph(ctx, "PUT", uploadURL, f, "application/octet-stream")
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
		if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
			return fmt.Errorf("OneDrive upload %s failed (HTTP %d)", remotePath, resp.StatusCode)
		}
		return nil
	}
	return c.uploadSession(ctx, f, total, item, remotePath)
}

func (c *OneDriveClient) uploadSession(ctx context.Context, f *os.File, total int64, item, remotePath string) error {
	sessionURL, err := c.createUploadSession(ctx, item)
	if err != nil {
		return err
	}
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
		contentRange := fmt.Sprintf("bytes %d-%d/%d", offset, offset+int64(n)-1, total)
		if err := c.putChunk(ctx, sessionURL, contentRange, buf[:n], total); err != nil {
			return err
		}
		offset += int64(n)
	}
	return nil
}

func (c *OneDriveClient) createUploadSession(ctx context.Context, item string) (string, error) {
	uploadURL := c.graphAPI("/me/drive/" + item + ":/createUploadSession")
	payload := `{"item": {"@microsoft.graph.conflictBehavior": "replace"}}`
	resp, err := c.doGraph(ctx, "POST", uploadURL, strings.NewReader(payload), "application/json")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("OneDrive createUploadSession failed (HTTP %d): %s", resp.StatusCode, truncateForLog(body))
	}
	var session struct {
		UploadURL string `json:"uploadUrl"`
	}
	if err := json.Unmarshal(body, &session); err != nil || session.UploadURL == "" {
		return "", fmt.Errorf("OneDrive upload session response parse failed: %w", err)
	}
	return session.UploadURL, nil
}

// putChunk uploads one chunk to the session URL. The session URL is
// pre-authorized: no Authorization header must be sent.
func (c *OneDriveClient) putChunk(ctx context.Context, sessionURL, contentRange string, chunk []byte, total int64) error {
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
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
		case resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusCreated || resp.StatusCode == http.StatusAccepted:
			return nil
		case resp.StatusCode == http.StatusRequestedRangeNotSatisfiable:
			// Chunk already recorded server-side; continue with the next range.
			return nil
		default:
			lastErr = fmt.Errorf("OneDrive chunk upload failed (HTTP %d, range %s)", resp.StatusCode, contentRange)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Duration(attempt+1) * 3 * time.Second):
			}
		}
	}
	_ = total
	return lastErr
}

func (c *OneDriveClient) DownloadFile(ctx context.Context, remotePath, localPath string) error {
	item := c.itemRef(remotePath)
	downloadURL := c.graphAPI("/me/drive/" + item + ":/content")
	resp, err := c.doGraph(ctx, "GET", downloadURL, nil, "")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		return fmt.Errorf("OneDrive download %s failed (HTTP %d): %s", remotePath, resp.StatusCode, truncateForLog(body))
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

func (c *OneDriveClient) DeleteFile(ctx context.Context, remotePath string) error {
	item := c.itemRef(remotePath)
	req, err := c.doGraph(ctx, "DELETE", c.graphAPI("/me/drive/"+item), nil, "")
	if err != nil {
		return err
	}
	defer req.Body.Close()
	io.Copy(io.Discard, io.LimitReader(req.Body, 1<<20))
	if req.StatusCode != http.StatusNoContent && req.StatusCode != http.StatusOK && req.StatusCode != http.StatusNotFound {
		return fmt.Errorf("OneDrive delete %s failed (HTTP %d)", remotePath, req.StatusCode)
	}
	return nil
}

// DeleteDir removes a remote folder tree in one Graph call.
func (c *OneDriveClient) DeleteDir(ctx context.Context, remotePath string) error {
	return c.DeleteFile(ctx, remotePath)
}

func truncateForLog(body []byte) string {
	const max = 300
	text := strings.TrimSpace(string(body))
	if len(text) > max {
		text = text[:max] + "..."
	}
	return text
}

func formatBytesShort(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.2f %cB", float64(n)/float64(div), "KMGTPE"[exp])
}
