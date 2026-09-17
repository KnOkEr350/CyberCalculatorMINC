package tests

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const smokeAdminEmail = "ci-admin@example.invalid"
const smokeAdminPassword = "ci-only-admin-password"

type composeSmoke struct {
	baseURL, project, root, version string
}

// These tests mutate a disposable Compose database, never the regular test DB
// or a production deployment. Ordinary go test runs skip them without opt-in.
func newComposeSmoke(t *testing.T) *composeSmoke {
	t.Helper()
	baseURL := os.Getenv("COMPOSE_SMOKE_URL")
	if baseURL == "" {
		t.Skip("COMPOSE_SMOKE_URL not set; requires an isolated Compose stack")
	}
	u, err := url.Parse(baseURL)
	if err != nil || u.Scheme != "http" || u.Hostname() != "127.0.0.1" || u.Port() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		t.Fatal("COMPOSE_SMOKE_URL must be http://127.0.0.1:<published nginx port>")
	}
	project := os.Getenv("COMPOSE_PROJECT_NAME")
	if !strings.HasPrefix(project, "cybercalc-ci-") || project == "cybercalc-ci-" {
		t.Fatal("COMPOSE_PROJECT_NAME must identify a disposable cybercalc-ci-* stack")
	}
	version := os.Getenv("APP_VERSION")
	if version == "" {
		t.Fatal("APP_VERSION is required to verify the deployed revision")
	}
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	s := &composeSmoke{strings.TrimRight(baseURL, "/"), project, root, version}
	_, port, err := net.SplitHostPort(strings.TrimSpace(s.compose(t, "", "port", "nginx", "80")))
	if err != nil || port != u.Port() {
		t.Fatal("COMPOSE_SMOKE_URL does not match this Compose project's published nginx port")
	}
	return s
}

func (s *composeSmoke) compose(t *testing.T, input string, args ...string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "docker", append([]string{
		"compose", "--project-directory", s.root, "--env-file", "/dev/null", "--project-name", s.project,
	}, args...)...)
	cmd.Stdin = strings.NewReader(input)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("docker compose %v: %v\n%s", args, err, out)
	}
	return string(out)
}

type smokeClient struct {
	baseURL string
	client  *http.Client
}

func (s *composeSmoke) newClient(t *testing.T) *smokeClient {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	return &smokeClient{s.baseURL, &http.Client{
		Jar: jar, Timeout: 20 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}
}

func (c *smokeClient) request(t *testing.T, method, path, contentType string, body io.Reader, want int) []byte {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), method, c.baseURL+path, body)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Cybercalc-Request", "1")
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	res, err := c.client.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer res.Body.Close()
	data, err := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	if err != nil {
		t.Fatalf("%s %s: reading response: %v", method, path, err)
	}
	if res.StatusCode != want {
		t.Fatalf("%s %s: got HTTP %d, want %d; response: %.2000s", method, path, res.StatusCode, want, data)
	}
	return data
}

func (c *smokeClient) json(t *testing.T, method, path string, payload any, want int) []byte {
	t.Helper()
	var body io.Reader
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		body = bytes.NewReader(data)
	}
	return c.request(t, method, path, "application/json", body, want)
}

func decodeSmoke[T any](t *testing.T, data []byte) T {
	t.Helper()
	var result T
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("decode response: %v; body: %.2000s", err, data)
	}
	return result
}

func (s *composeSmoke) login(t *testing.T, email, password string) *smokeClient {
	t.Helper()
	c := s.newClient(t)
	c.json(t, "POST", "/api/auth/login", map[string]string{"email": email, "password": password}, http.StatusOK)
	profile := decodeSmoke[struct{ Email string }](t, c.json(t, "GET", "/api/auth/me", nil, http.StatusOK))
	if profile.Email != email {
		t.Fatalf("authenticated as %q, want %q", profile.Email, email)
	}
	return c
}

func (c *smokeClient) upload(t *testing.T, entryID, field, name, content string) {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile(field, name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(part, content); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	c.request(t, "POST", "/api/entries/"+entryID+"/attachments", writer.FormDataContentType(), &body, http.StatusCreated)
}
