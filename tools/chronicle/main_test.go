package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const testAsset = "https://github.com/user-attachments/assets/12345678-1234-1234-1234-123456789abc"
const testIssue = "https://api.github.com/repos/drawmeanelephant/VirelaiOS/issues/716"

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func response(code int, body string, headers map[string]string) *http.Response {
	h := make(http.Header)
	for k, v := range headers {
		h.Set(k, v)
	}
	return &http.Response{StatusCode: code, Body: io.NopCloser(strings.NewReader(body)), Header: h}
}

type harness struct {
	t            *testing.T
	p            publisher
	o            options
	saved        comment
	image        []byte
	events       []string
	uploadStatus int
	postSave404s int
	apiFailure   bool
	objectType   string
	location     string
	push         bool
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	dir := t.TempDir()
	// Only a small content-sniff vector is needed, not a third-party image.
	image := append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 64)...)
	path := filepath.Join(dir, "banner & plate.png")
	if err := os.WriteFile(path, image, 0600); err != nil {
		t.Fatal(err)
	}
	h := &harness{
		t: t, image: image, uploadStatus: 201, objectType: "image/png", push: true,
		location: "https://example.s3.amazonaws.com/banner?X-Amz-Signature=test",
		o: options{Repo: "drawmeanelephant/VirelaiOS", Entry: 716, Image: path,
			Caption: "A [banner] & <plate>", State: filepath.Join(dir, "receipt.json"), Endpoint: uploadEndpoint},
	}
	h.p = publisher{api: h.api, token: "test-credential", pause: func() {},
		client: &http.Client{Transport: transportFunc(h.roundTrip), CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		}}}
	return h
}

func decodeInto(data, out any) error {
	b, err := json.Marshal(data)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, out)
}

func (h *harness) api(method, path string, input, output any) error {
	h.events = append(h.events, "api:"+method)
	switch {
	case path == "repos/"+h.o.Repo:
		return decodeInto(map[string]any{"id": 1324482513, "permissions": map[string]bool{"push": h.push}}, output)
	case strings.Contains(path, "/issues/716/comments?"):
		var list []comment
		if h.saved.ID != 0 {
			list = []comment{h.saved}
		}
		return decodeInto(list, output)
	case method == "POST" || method == "PATCH":
		if h.apiFailure {
			return errors.New("simulated save failure")
		}
		h.saved = comment{ID: 123, Body: input.(map[string]string)["body"], IssueURL: testIssue}
		h.events = append(h.events, "save")
		return decodeInto(h.saved, output)
	case method == "GET" && strings.Contains(path, "/issues/comments/"):
		h.events = append(h.events, "read-body")
		return decodeInto(h.saved, output)
	default:
		return fmt.Errorf("unexpected API call %s %s", method, path)
	}
}

func (h *harness) roundTrip(r *http.Request) (*http.Response, error) {
	switch r.URL.Host {
	case "uploads.github.com":
		h.events = append(h.events, "upload")
		if r.Method != "POST" || r.Header.Get("Authorization") != "Bearer test-credential" {
			h.t.Fatal("upload is not a Bearer POST")
		}
		q := r.URL.Query()
		if q.Get("repository_id") != "1324482513" || q.Get("name") != filepath.Base(h.o.Image) || q.Get("content_type") != "image/png" {
			h.t.Fatal("incorrect or unescaped upload parameters")
		}
		return response(h.uploadStatus, `{"url":"`+testAsset+`"}`, nil), nil
	case "github.com":
		h.events = append(h.events, "resolve")
		if !strings.Contains(h.saved.Body, testAsset) {
			h.t.Fatal("resolution attempted before saving the body")
		}
		if r.Header.Get("Authorization") != "Bearer test-credential" {
			h.t.Fatal("attachment viewer was not authenticated")
		}
		if h.postSave404s > 0 {
			h.postSave404s--
			return response(404, "", nil), nil
		}
		return response(302, "", map[string]string{"Location": h.location}), nil
	case "example.s3.amazonaws.com":
		h.events = append(h.events, "image")
		if r.Header.Get("Authorization") != "" {
			h.t.Fatal("GitHub token leaked to S3")
		}
		return response(200, string(h.image), map[string]string{"Content-Type": h.objectType}), nil
	default:
		return nil, errors.New("unreachable endpoint")
	}
}

func TestPublishOrderingAndReceiptReuse(t *testing.T) {
	h := newHarness(t)
	h.postSave404s = 1
	line, err := h.p.publish(h.o)
	if err != nil {
		t.Fatal(err)
	}
	want := "![A \\[banner\\] &amp; &lt;plate&gt;](" + testAsset + ")"
	if line != want {
		t.Fatalf("line = %q", line)
	}
	events := strings.Join(h.events, ",")
	if !strings.Contains(events, "save,api:GET,read-body,resolve,resolve,image") {
		t.Fatalf("wrong ordering: %s", events)
	}
	info, err := os.Stat(h.o.State)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("receipt must exist and be private")
	}
	h.events = nil
	if _, err := h.p.publish(h.o); err != nil {
		t.Fatal(err)
	}
	for _, event := range h.events {
		if event == "upload" || event == "save" {
			t.Fatalf("retry duplicated remote work: %v", h.events)
		}
	}
}

func TestUploadFailuresReturnNoMarkdown(t *testing.T) {
	for _, status := range []int{404, 422, 500, 302} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			h := newHarness(t)
			h.uploadStatus = status
			line, err := h.p.publish(h.o)
			if err == nil || line != "" || strings.Contains(err.Error(), testAsset) {
				t.Fatalf("failure leaked a URL: line=%q err=%v", line, err)
			}
			if (status == 404 || status == 422) && !strings.Contains(err.Error(), "STOP") {
				t.Fatal("endpoint drift must stop the batch")
			}
			if strings.Contains(strings.Join(h.events, ","), "resolve") || h.saved.ID != 0 {
				t.Fatal("failed upload saved a body or checked an asset")
			}
			h.events = nil
			if _, err := h.p.publish(h.o); err == nil || len(h.events) != 0 {
				t.Fatal("an ambiguous attempted upload must not be silently repeated")
			}
		})
	}
}

func TestUnreachableEndpointReturnsNoMarkdown(t *testing.T) {
	h := newHarness(t)
	h.o.Endpoint = "http://127.0.0.1:1/user-attachments/assets"
	line, err := h.p.publish(h.o)
	if err == nil || line != "" || strings.Contains(err.Error(), "http") {
		t.Fatalf("line=%q err=%v", line, err)
	}
}

func TestSaveFailureRetriesWithoutUpload(t *testing.T) {
	h := newHarness(t)
	h.apiFailure = true
	if line, err := h.p.publish(h.o); err == nil || line != "" {
		t.Fatal("save failure must not print markdown")
	}
	h.apiFailure = false
	h.events = nil
	if _, err := h.p.publish(h.o); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.Join(h.events, ","), "upload") {
		t.Fatal("save retry uploaded again")
	}
}

func TestPostSave404RetriesWithoutUpload(t *testing.T) {
	h := newHarness(t)
	h.postSave404s = 4
	if line, err := h.p.publish(h.o); err == nil || line != "" {
		t.Fatal("unresolved image must not print markdown")
	}
	h.events = nil
	if _, err := h.p.publish(h.o); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.Join(h.events, ","), "upload") {
		t.Fatal("post-save 404 caused a re-upload")
	}
}

func TestExistingProseIsPreserved(t *testing.T) {
	h := newHarness(t)
	h.o.Comment = 123
	h.saved = comment{ID: 123, Body: "Original chronicle prose.\n\n## Evidence\nUnchanged.", IssueURL: testIssue}
	prose := h.saved.Body
	line, err := h.p.publish(h.o)
	if err != nil {
		t.Fatal(err)
	}
	if h.saved.Body != prose+"\n\n"+line {
		t.Fatal("publisher changed the prose")
	}
}

func TestWrongEntryCommentIsNotEdited(t *testing.T) {
	h := newHarness(t)
	h.o.Comment = 123
	h.saved = comment{ID: 123, Body: "Other entry", IssueURL: testIssue + "0"}
	if line, err := h.p.publish(h.o); err == nil || line != "" || h.saved.Body != "Other entry" {
		t.Fatal("wrong entry's comment must be rejected")
	}
}

func TestReceiptMismatchRefusesRemoteWork(t *testing.T) {
	h := newHarness(t)
	if _, err := h.p.publish(h.o); err != nil {
		t.Fatal(err)
	}
	h.o.Caption = "Different caption"
	h.events = nil
	if _, err := h.p.publish(h.o); err == nil || len(h.events) != 0 {
		t.Fatal("mismatched receipt must be rejected before any remote work")
	}
}

func TestRejectsNonImagesAndMissingPushAccess(t *testing.T) {
	h := newHarness(t)
	h.push = false
	if line, err := h.p.publish(h.o); err == nil || line != "" {
		t.Fatal("missing push permission must fail")
	}
	if strings.Contains(strings.Join(h.events, ","), "upload") {
		t.Fatal("uploaded without push access")
	}
	if _, err := os.Stat(h.o.State); !os.IsNotExist(err) {
		t.Fatal("a permission failure must not record an upload attempt")
	}
	h = newHarness(t)
	if err := os.WriteFile(h.o.Image, []byte("%PDF-1.7\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := h.p.publish(h.o); err == nil || len(h.events) != 0 {
		t.Fatal("a PDF named PNG must be rejected")
	}
}

func TestReceiptLockRefusesConcurrentPublisher(t *testing.T) {
	h := newHarness(t)
	path := h.o.State + ".lock"
	if err := os.WriteFile(path, []byte("held"), 0600); err != nil {
		t.Fatal(err)
	}
	if line, err := h.p.publish(h.o); err == nil || line != "" || len(h.events) != 0 {
		t.Fatal("a concurrent publisher must be refused before remote work")
	}
	b, err := os.ReadFile(path)
	if err != nil || string(b) != "held" {
		t.Fatal("must not remove or overwrite another publisher's lock")
	}
}

func TestVerificationRefusesUnsafeRedirectsAndWrongContent(t *testing.T) {
	for _, location := range []string{
		"https://evil.example/image?X-Amz-Signature=test",
		"http://example.s3.amazonaws.com/image?X-Amz-Signature=test",
		"https://example.s3.amazonaws.com/image",
		"https://user:pass@example.s3.amazonaws.com/image?X-Amz-Signature=test",
	} {
		t.Run(location, func(t *testing.T) {
			h := newHarness(t)
			h.location = location
			if line, err := h.p.publish(h.o); err == nil || line != "" {
				t.Fatal("unsafe redirect accepted")
			}
		})
	}
	h := newHarness(t)
	h.objectType = "text/html"
	if line, err := h.p.publish(h.o); err == nil || line != "" {
		t.Fatal("non-image content accepted")
	}
}

func TestEndpointOverrideCannotExfiltrateCredential(t *testing.T) {
	h := newHarness(t)
	for _, endpoint := range []string{
		"https://evil.example/upload", "http://uploads.github.com/user-attachments/assets",
		"https://uploads.github.com/user-attachments/other",
		"http://user:pass@127.0.0.1/upload", "http://127.0.0.1/upload?token=foo",
	} {
		h.o.Endpoint = endpoint
		if _, err := h.p.publish(h.o); err == nil {
			t.Fatalf("accepted unsafe override %q", endpoint)
		}
	}
	if len(h.events) != 0 {
		t.Fatal("unsafe override made a remote call")
	}
}
