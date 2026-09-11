package remote

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

// StorageClient defines the interface for remote storage backends (SFTP, WebDAV, MinIO/S3).
type StorageClient interface {
	TestConnection(ctx context.Context) error
	UploadFile(ctx context.Context, localPath, remotePath string) error
	DownloadFile(ctx context.Context, remotePath, localPath string) error
	DeleteFile(ctx context.Context, remotePath string) error
}

// NewClient creates a StorageClient based on pool type and config.
func NewClient(storageType string, config map[string]string) (StorageClient, error) {
	switch strings.ToLower(strings.TrimSpace(storageType)) {
	case "sftp":
		return NewSFTPClient(config)
	case "webdav":
		return NewWebDAVClient(config)
	case "minio", "s3":
		return NewMinIOClient(config)
	default:
		return nil, fmt.Errorf("unsupported remote storage type: %s", storageType)
	}
}

// =========================================================================
// SFTP Client Implementation (SSH subsystem)
// =========================================================================

type SFTPClient struct {
	Host     string
	Port     int
	User     string
	Password string
	Key      string
	BasePath string
}

func NewSFTPClient(cfg map[string]string) (*SFTPClient, error) {
	host := strings.TrimSpace(cfg["host"])
	if host == "" {
		return nil, fmt.Errorf("SFTP host is required")
	}
	port := 22
	if p, err := strconv.Atoi(cfg["port"]); err == nil && p > 0 {
		port = p
	}
	user := strings.TrimSpace(cfg["user"])
	if user == "" {
		user = "root"
	}
	basePath := strings.TrimSpace(cfg["base_path"])
	if basePath == "" {
		basePath = "/clicd-backups"
	}
	return &SFTPClient{
		Host:     host,
		Port:     port,
		User:     user,
		Password: cfg["password"],
		Key:      cfg["key"],
		BasePath: basePath,
	}, nil
}

func (c *SFTPClient) sshConfig() (*ssh.ClientConfig, error) {
	authMethods := make([]ssh.AuthMethod, 0)
	if c.Password != "" {
		authMethods = append(authMethods, ssh.Password(c.Password))
	}
	if strings.TrimSpace(c.Key) != "" {
		signer, err := ssh.ParsePrivateKey([]byte(c.Key))
		if err == nil {
			authMethods = append(authMethods, ssh.PublicKeys(signer))
		}
	}
	if len(authMethods) == 0 {
		return nil, fmt.Errorf("SFTP password or private key is required")
	}
	return &ssh.ClientConfig{
		User:            c.User,
		Auth:            authMethods,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         10 * time.Second,
	}, nil
}

func (c *SFTPClient) TestConnection(ctx context.Context) error {
	cfg, err := c.sshConfig()
	if err != nil {
		return err
	}
	addr := fmt.Sprintf("%s:%d", c.Host, c.Port)
	client, err := ssh.Dial("tcp", addr, cfg)
	if err != nil {
		return fmt.Errorf("SFTP connection failed to %s: %w", addr, err)
	}
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		return fmt.Errorf("SSH session open failed: %w", err)
	}
	defer session.Close()

	return session.Run("echo clicd-sftp-test")
}

func (c *SFTPClient) UploadFile(ctx context.Context, localPath, remotePath string) error {
	cfg, err := c.sshConfig()
	if err != nil {
		return err
	}
	client, err := ssh.Dial("tcp", fmt.Sprintf("%s:%d", c.Host, c.Port), cfg)
	if err != nil {
		return err
	}
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		return err
	}
	defer session.Close()

	fullRemote := path.Join(c.BasePath, remotePath)
	dir := path.Dir(fullRemote)

	// Ensure remote directory exists
	mkdirSession, _ := client.NewSession()
	if mkdirSession != nil {
		_ = mkdirSession.Run(fmt.Sprintf("mkdir -p %q", dir))
		mkdirSession.Close()
	}

	srcFile, err := os.Open(localPath)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	stat, err := srcFile.Stat()
	if err != nil {
		return err
	}

	// Stream file via cat > remotePath
	stdin, err := session.StdinPipe()
	if err != nil {
		return err
	}

	cmd := fmt.Sprintf("cat > %q", fullRemote)
	if err := session.Start(cmd); err != nil {
		return err
	}

	_ = stat
	if _, err := io.Copy(stdin, srcFile); err != nil {
		stdin.Close()
		return err
	}
	stdin.Close()

	return session.Wait()
}

func (c *SFTPClient) DownloadFile(ctx context.Context, remotePath, localPath string) error {
	cfg, err := c.sshConfig()
	if err != nil {
		return err
	}
	client, err := ssh.Dial("tcp", fmt.Sprintf("%s:%d", c.Host, c.Port), cfg)
	if err != nil {
		return err
	}
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		return err
	}
	defer session.Close()

	fullRemote := path.Join(c.BasePath, remotePath)
	_ = os.MkdirAll(filepath.Dir(localPath), 0755)

	dstFile, err := os.Create(localPath)
	if err != nil {
		return err
	}
	defer dstFile.Close()

	session.Stdout = dstFile
	cmd := fmt.Sprintf("cat %q", fullRemote)
	return session.Run(cmd)
}

func (c *SFTPClient) DeleteFile(ctx context.Context, remotePath string) error {
	cfg, err := c.sshConfig()
	if err != nil {
		return err
	}
	client, err := ssh.Dial("tcp", fmt.Sprintf("%s:%d", c.Host, c.Port), cfg)
	if err != nil {
		return err
	}
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		return err
	}
	defer session.Close()

	fullRemote := path.Join(c.BasePath, remotePath)
	return session.Run(fmt.Sprintf("rm -rf %q", fullRemote))
}

// =========================================================================
// WebDAV Client Implementation
// =========================================================================

type WebDAVClient struct {
	URL      string
	User     string
	Password string
	HTTP     *http.Client
}

func NewWebDAVClient(cfg map[string]string) (*WebDAVClient, error) {
	rawURL := strings.TrimSpace(cfg["url"])
	if rawURL == "" {
		rawURL = strings.TrimSpace(cfg["endpoint"])
	}
	if rawURL == "" {
		return nil, fmt.Errorf("WebDAV URL is required")
	}
	if !strings.HasPrefix(rawURL, "http://") && !strings.HasPrefix(rawURL, "https://") {
		rawURL = "http://" + rawURL
	}
	return &WebDAVClient{
		URL:      strings.TrimRight(rawURL, "/"),
		User:     cfg["user"],
		Password: cfg["password"],
		HTTP:     &http.Client{Timeout: 60 * time.Second},
	}, nil
}

func (w *WebDAVClient) setAuth(req *http.Request) {
	if w.User != "" {
		req.SetBasicAuth(w.User, w.Password)
	}
}

func (w *WebDAVClient) TestConnection(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, "PROPFIND", w.URL+"/", nil)
	if err != nil {
		return err
	}
	w.setAuth(req)
	req.Header.Set("Depth", "0")
	resp, err := w.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("WebDAV request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 207 && resp.StatusCode != 200 && resp.StatusCode != 204 {
		return fmt.Errorf("WebDAV PROPFIND returned unexpected status: %s", resp.Status)
	}
	return nil
}

func (w *WebDAVClient) ensureDirectory(ctx context.Context, targetURL string) {
	u, err := url.Parse(targetURL)
	if err != nil {
		return
	}
	segments := strings.Split(strings.Trim(u.Path, "/"), "/")
	current := u.Scheme + "://" + u.Host
	for _, seg := range segments {
		if seg == "" {
			continue
		}
		current += "/" + seg
		req, _ := http.NewRequestWithContext(ctx, "MKCOL", current, nil)
		if req != nil {
			w.setAuth(req)
			resp, err := w.HTTP.Do(req)
			if err == nil {
				resp.Body.Close()
			}
		}
	}
}

func (w *WebDAVClient) UploadFile(ctx context.Context, localPath, remotePath string) error {
	file, err := os.Open(localPath)
	if err != nil {
		return err
	}
	defer file.Close()

	fullURL := w.URL + "/" + strings.TrimLeft(remotePath, "/")
	w.ensureDirectory(ctx, fullURL[:strings.LastIndex(fullURL, "/")])

	req, err := http.NewRequestWithContext(ctx, "PUT", fullURL, file)
	if err != nil {
		return err
	}
	w.setAuth(req)
	req.Header.Set("Content-Type", "application/octet-stream")

	resp, err := w.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("WebDAV upload returned status: %s", resp.Status)
	}
	return nil
}

func (w *WebDAVClient) DownloadFile(ctx context.Context, remotePath, localPath string) error {
	fullURL := w.URL + "/" + strings.TrimLeft(remotePath, "/")
	req, err := http.NewRequestWithContext(ctx, "GET", fullURL, nil)
	if err != nil {
		return err
	}
	w.setAuth(req)

	resp, err := w.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("WebDAV download failed with status: %s", resp.Status)
	}

	_ = os.MkdirAll(filepath.Dir(localPath), 0755)
	dst, err := os.Create(localPath)
	if err != nil {
		return err
	}
	defer dst.Close()

	_, err = io.Copy(dst, resp.Body)
	return err
}

func (w *WebDAVClient) DeleteFile(ctx context.Context, remotePath string) error {
	fullURL := w.URL + "/" + strings.TrimLeft(remotePath, "/")
	req, err := http.NewRequestWithContext(ctx, "DELETE", fullURL, nil)
	if err != nil {
		return err
	}
	w.setAuth(req)
	resp, err := w.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}

// =========================================================================
// MinIO / AWS S3 Compatible Client Implementation (S3 Signature V4)
// =========================================================================

type MinIOClient struct {
	Endpoint  string
	Bucket    string
	AccessKey string
	SecretKey string
	Region    string
	UseSSL    bool
	HTTP      *http.Client
}

func NewMinIOClient(cfg map[string]string) (*MinIOClient, error) {
	endpoint := strings.TrimSpace(cfg["endpoint"])
	if endpoint == "" {
		endpoint = strings.TrimSpace(cfg["host"])
	}
	if endpoint == "" {
		return nil, fmt.Errorf("MinIO/S3 endpoint is required")
	}
	endpoint = strings.TrimPrefix(strings.TrimPrefix(endpoint, "http://"), "https://")

	bucket := strings.TrimSpace(cfg["bucket"])
	if bucket == "" {
		bucket = "clicd"
	}
	region := strings.TrimSpace(cfg["region"])
	if region == "" {
		region = "us-east-1"
	}
	useSSL := true
	if strings.ToLower(cfg["use_ssl"]) == "false" || strings.ToLower(cfg["ssl"]) == "false" {
		useSSL = false
	}

	return &MinIOClient{
		Endpoint:  endpoint,
		Bucket:    bucket,
		AccessKey: cfg["access_key"],
		SecretKey: cfg["secret_key"],
		Region:    region,
		UseSSL:    useSSL,
		HTTP:      &http.Client{Timeout: 120 * time.Second},
	}, nil
}

func (m *MinIOClient) signS3V4(req *http.Request, payloadHash string) {
	t := time.Now().UTC()
	dateStamp := t.Format("20060102")
	amzDate := t.Format("20060102T150405Z")

	req.Header.Set("x-amz-date", amzDate)
	req.Header.Set("x-amz-content-sha256", payloadHash)

	canonicalURI := req.URL.EscapedPath()
	if canonicalURI == "" {
		canonicalURI = "/"
	}
	canonicalQuery := req.URL.RawQuery
	canonicalHeaders := fmt.Sprintf("host:%s\nx-amz-content-sha256:%s\nx-amz-date:%s\n", req.Host, payloadHash, amzDate)
	signedHeaders := "host;x-amz-content-sha256;x-amz-date"

	canonicalReq := fmt.Sprintf("%s\n%s\n%s\n%s\n%s\n%s", req.Method, canonicalURI, canonicalQuery, canonicalHeaders, signedHeaders, payloadHash)
	h := sha256.New()
	h.Write([]byte(canonicalReq))
	canonicalReqHash := hex.EncodeToString(h.Sum(nil))

	credentialScope := fmt.Sprintf("%s/%s/s3/aws4_request", dateStamp, m.Region)
	stringToSign := fmt.Sprintf("AWS4-HMAC-SHA256\n%s\n%s\n%s", amzDate, credentialScope, canonicalReqHash)

	kDate := hmacSHA256([]byte("AWS4"+m.SecretKey), []byte(dateStamp))
	kRegion := hmacSHA256(kDate, []byte(m.Region))
	kService := hmacSHA256(kRegion, []byte("s3"))
	kSigning := hmacSHA256(kService, []byte("aws4_request"))
	signature := hex.EncodeToString(hmacSHA256(kSigning, []byte(stringToSign)))

	authHeader := fmt.Sprintf("AWS4-HMAC-SHA256 Credential=%s/%s, SignedHeaders=%s, Signature=%s",
		m.AccessKey, credentialScope, signedHeaders, signature)
	req.Header.Set("Authorization", authHeader)
}

func hmacSHA256(key, data []byte) []byte {
	h := hmac.New(sha256.New, key)
	h.Write(data)
	return h.Sum(nil)
}

func (m *MinIOClient) TestConnection(ctx context.Context) error {
	scheme := "https"
	if !m.UseSSL {
		scheme = "http"
	}
	urlStr := fmt.Sprintf("%s://%s/%s", scheme, m.Endpoint, m.Bucket)
	req, err := http.NewRequestWithContext(ctx, "HEAD", urlStr, nil)
	if err != nil {
		return err
	}
	req.Host = m.Endpoint

	emptyHash := "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	m.signS3V4(req, emptyHash)

	resp, err := m.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("S3/MinIO connect failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == 404 {
		// Bucket does not exist, try creating it with PUT
		putReq, _ := http.NewRequestWithContext(ctx, "PUT", urlStr, nil)
		if putReq != nil {
			putReq.Host = m.Endpoint
			m.signS3V4(putReq, emptyHash)
			putResp, putErr := m.HTTP.Do(putReq)
			if putErr == nil {
				putResp.Body.Close()
				if putResp.StatusCode == 200 || putResp.StatusCode == 204 {
					return nil
				}
			}
		}
	}
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	return fmt.Errorf("S3 head bucket returned HTTP %d (%s)", resp.StatusCode, resp.Status)
}

func (m *MinIOClient) UploadFile(ctx context.Context, localPath, remotePath string) error {
	data, err := os.ReadFile(localPath)
	if err != nil {
		return err
	}

	h := sha256.New()
	h.Write(data)
	payloadHash := hex.EncodeToString(h.Sum(nil))

	scheme := "https"
	if !m.UseSSL {
		scheme = "http"
	}
	objectKey := strings.TrimLeft(remotePath, "/")
	urlStr := fmt.Sprintf("%s://%s/%s/%s", scheme, m.Endpoint, m.Bucket, objectKey)

	req, err := http.NewRequestWithContext(ctx, "PUT", urlStr, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Host = m.Endpoint
	req.Header.Set("Content-Type", "application/octet-stream")
	m.signS3V4(req, payloadHash)

	resp, err := m.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	return fmt.Errorf("S3 PUT object failed: HTTP %s", resp.Status)
}

func (m *MinIOClient) DownloadFile(ctx context.Context, remotePath, localPath string) error {
	scheme := "https"
	if !m.UseSSL {
		scheme = "http"
	}
	objectKey := strings.TrimLeft(remotePath, "/")
	urlStr := fmt.Sprintf("%s://%s/%s/%s", scheme, m.Endpoint, m.Bucket, objectKey)

	req, err := http.NewRequestWithContext(ctx, "GET", urlStr, nil)
	if err != nil {
		return err
	}
	req.Host = m.Endpoint

	emptyHash := "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	m.signS3V4(req, emptyHash)

	resp, err := m.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("S3 GET object failed: HTTP %s", resp.Status)
	}

	_ = os.MkdirAll(filepath.Dir(localPath), 0755)
	dst, err := os.Create(localPath)
	if err != nil {
		return err
	}
	defer dst.Close()

	_, err = io.Copy(dst, resp.Body)
	return err
}

func (m *MinIOClient) DeleteFile(ctx context.Context, remotePath string) error {
	scheme := "https"
	if !m.UseSSL {
		scheme = "http"
	}
	objectKey := strings.TrimLeft(remotePath, "/")
	urlStr := fmt.Sprintf("%s://%s/%s/%s", scheme, m.Endpoint, m.Bucket, objectKey)

	req, err := http.NewRequestWithContext(ctx, "DELETE", urlStr, nil)
	if err != nil {
		return err
	}
	req.Host = m.Endpoint

	emptyHash := "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	m.signS3V4(req, emptyHash)

	resp, err := m.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}
