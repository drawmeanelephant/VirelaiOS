// Chronicle publishes image-only banners without moving the prose entries.
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const uploadEndpoint = "https://uploads.github.com/user-attachments/assets"

var assetURL = regexp.MustCompile(`^https://github\.com/user-attachments/assets/[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
var repoName = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)

type options struct {
	Repo, Image, Caption, State, Endpoint string
	Entry, Comment                        int64
}

type receipt struct {
	Repo, SHA256, Caption, URL string
	Entry, Target, Comment     int64
}

type comment struct {
	ID       int64  `json:"id"`
	Body     string `json:"body"`
	IssueURL string `json:"issue_url"`
}

type apiCall func(method, path string, input, output any) error

type publisher struct {
	api    apiCall
	client *http.Client
	token  string
	pause  func()
	log    io.Writer
}

func main() {
	o := options{}
	flag.StringVar(&o.Repo, "repo", "drawmeanelephant/VirelaiOS", "repository owner/name")
	flag.Int64Var(&o.Entry, "entry", 0, "chronicle entry's PR or issue number (required)")
	flag.Int64Var(&o.Comment, "comment", 0, "existing chronicle comment ID; otherwise create a banner-only comment")
	flag.StringVar(&o.State, "state", "", "durable upload receipt path (required; reuse to retry without uploading)")
	flag.StringVar(&o.Endpoint, "upload-endpoint", uploadEndpoint, "upload endpoint; only GitHub or loopback test endpoints")
	flag.Usage = func() {
		fmt.Fprintln(flag.CommandLine.Output(), "Usage: chronicle --entry N --state PATH [--comment ID] IMAGE CAPTION")
		flag.PrintDefaults()
	}
	flag.Parse()
	if flag.NArg() != 2 {
		flag.Usage()
		os.Exit(2)
	}
	o.Image, o.Caption = flag.Arg(0), flag.Arg(1)
	if err := validateOptions(o); err != nil {
		fmt.Fprintln(os.Stderr, "chronicle:", err)
		os.Exit(2)
	}
	// Capture the credential in memory, never in arguments, receipts or logs.
	token, err := exec.Command("gh", "auth", "token", "--hostname", "github.com").Output()
	if err != nil || strings.TrimSpace(string(token)) == "" {
		fmt.Fprintln(os.Stderr, "chronicle: gh authentication unavailable")
		os.Exit(1)
	}
	p := publisher{
		api: ghAPI, token: strings.TrimSpace(string(token)),
		client: &http.Client{Timeout: 60 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		}},
		pause: func() { time.Sleep(time.Second) }, log: os.Stderr,
	}
	line, err := p.publish(o)
	if err != nil {
		fmt.Fprintln(os.Stderr, "chronicle:", err)
		os.Exit(1)
	}
	fmt.Println(line)
}

func validateOptions(o options) error {
	if !repoName.MatchString(o.Repo) || o.Entry <= 0 || o.Comment < 0 || o.State == "" {
		return errors.New("a repository, positive --entry and --state path are required")
	}
	if strings.TrimSpace(o.Caption) == "" || strings.ContainsAny(o.Caption, "\r\n") {
		return errors.New("caption must be one nonempty line")
	}
	u, err := url.Parse(o.Endpoint)
	if err != nil || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return errors.New("invalid upload endpoint")
	}
	loopback := u.Hostname() == "127.0.0.1" || u.Hostname() == "::1"
	if o.Endpoint != uploadEndpoint && !(loopback && u.Scheme == "http") {
		return errors.New("upload endpoint must be GitHub or HTTP loopback for testing")
	}
	return nil
}

func ghAPI(method, path string, input, output any) error {
	args := []string{"api", "--hostname", "github.com", "--method", method, path}
	var stdin io.Reader
	if input != nil {
		b, err := json.Marshal(input)
		if err != nil {
			return err
		}
		stdin = bytes.NewReader(b)
		args = append(args, "--input", "-")
	}
	cmd := exec.Command("gh", args...)
	cmd.Stdin = stdin
	b, err := cmd.Output()
	if err != nil {
		// Do not echo command output, a body, or authentication material.
		return fmt.Errorf("GitHub API %s failed", method)
	}
	if output != nil {
		if err := json.Unmarshal(b, output); err != nil {
			return errors.New("invalid GitHub API response")
		}
	}
	return nil
}

func imageData(path string) ([]byte, string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, "", errors.New("cannot read image")
	}
	mime := http.DetectContentType(data)
	switch mime {
	case "image/png", "image/jpeg", "image/gif", "image/webp":
		return data, mime, nil
	default:
		return nil, "", errors.New("only PNG, JPEG, GIF and WebP images are accepted (checked by content)")
	}
}

func markdown(caption, asset string) string {
	escape := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", "\\", "\\\\", "[", "\\[", "]", "\\]")
	return "![" + escape.Replace(caption) + "](" + asset + ")"
}

func (p publisher) publish(o options) (string, error) {
	if err := validateOptions(o); err != nil {
		return "", err
	}
	data, mime, err := imageData(o.Image)
	if err != nil {
		return "", err
	}
	lock, err := os.OpenFile(o.State+".lock", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return "", errors.New("cannot lock receipt; parent must exist and another publisher must not hold the lock")
	}
	lock.Close()
	defer os.Remove(o.State + ".lock")
	want := receipt{Repo: o.Repo, Entry: o.Entry, Target: o.Comment, Caption: o.Caption, SHA256: fmt.Sprintf("%x", sha256.Sum256(data))}
	r := want
	state, err := os.ReadFile(o.State)
	switch {
	case err == nil:
		if json.Unmarshal(state, &r) != nil || r.Repo != want.Repo || r.Entry != want.Entry ||
			r.Target != want.Target || r.Caption != want.Caption || r.SHA256 != want.SHA256 ||
			(r.URL != "" && !assetURL.MatchString(r.URL)) || r.Comment < 0 {
			return "", errors.New("receipt does not match this image, caption and entry; refusing another upload")
		}
	case !errors.Is(err, os.ErrNotExist):
		return "", errors.New("cannot read receipt; refusing upload")
	}
	// A lost response cannot establish whether the server created an asset.
	// Keep an attempted-upload receipt rather than silently duplicating it.
	if r.URL == "" && err == nil {
		return "", errors.New("incomplete receipt; inspect it before retrying")
	}
	var repo struct {
		ID          int64 `json:"id"`
		Permissions struct {
			Push bool `json:"push"`
		} `json:"permissions"`
	}
	if err := p.api("GET", "repos/"+o.Repo, nil, &repo); err != nil {
		return "", err
	}
	if repo.ID <= 0 || !repo.Permissions.Push {
		return "", errors.New("token must have push access and the repository must have a numeric ID")
	}
	if r.URL == "" {
		// Persist the attempt before the request: a lost response is ambiguous.
		if err := storeReceipt(o.State, r); err != nil {
			return "", err
		}
		r.URL, err = p.upload(o, repo.ID, data, mime)
		if err != nil {
			return "", err
		}
		if err := storeReceipt(o.State, r); err != nil {
			return "", errors.New("upload succeeded but receipt save failed; stop, do not re-upload")
		}
	}
	line := markdown(o.Caption, r.URL)
	if err := p.claim(o, &r, line); err != nil {
		return "", fmt.Errorf("%w; keep --state and retry without re-uploading", err)
	}
	if err := storeReceipt(o.State, r); err != nil {
		return "", err
	}
	// There is deliberately no resolution request before the body is saved.
	if err := p.verify(r.URL, mime); err != nil {
		return "", fmt.Errorf("%w; body saved, keep --state and re-check without re-uploading", err)
	}
	if p.log != nil {
		fmt.Fprintf(p.log, "chronicle: verified entry #%d comment %d: authenticated HTTP 302 -> signed S3 HTTP 200 (%s)\n", o.Entry, r.Comment, mime)
	}
	return line, nil
}

func storeReceipt(path string, r receipt) error {
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".chronicle-*")
	if err != nil {
		return errors.New("cannot create receipt; parent directory must exist")
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(append(b, '\n')); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil || closeErr != nil {
		return errors.New("cannot write receipt")
	}
	if err := os.Rename(f.Name(), path); err != nil {
		return errors.New("cannot publish receipt")
	}
	return nil
}

func (p publisher) upload(o options, rid int64, data []byte, mime string) (string, error) {
	u, _ := url.Parse(o.Endpoint)
	q := u.Query()
	q.Set("name", filepath.Base(o.Image))
	q.Set("content_type", mime)
	q.Set("repository_id", strconv.FormatInt(rid, 10))
	u.RawQuery = q.Encode()
	req, _ := http.NewRequest("POST", u.String(), bytes.NewReader(data))
	req.Header.Set("Authorization", "Bearer "+p.token)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", mime)
	res, err := p.client.Do(req)
	if err != nil {
		return "", errors.New("upload request failed; no URL emitted")
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusCreated {
		if res.StatusCode == 404 || res.StatusCode == 422 {
			return "", fmt.Errorf("upload returned HTTP %d; STOP and report on #1977; do not try a replacement endpoint", res.StatusCode)
		}
		return "", fmt.Errorf("upload returned HTTP %d; no URL emitted", res.StatusCode)
	}
	var result struct {
		URL string `json:"url"`
	}
	if json.NewDecoder(io.LimitReader(res.Body, 4096)).Decode(&result) != nil || !assetURL.MatchString(result.URL) {
		return "", errors.New("upload returned an invalid attachment URL; stop, do not re-upload")
	}
	return result.URL, nil
}

func (p publisher) claim(o options, r *receipt, line string) error {
	base := "repos/" + o.Repo
	issueURL := "https://api.github.com/" + base + "/issues/" + strconv.FormatInt(o.Entry, 10)
	id := r.Comment
	if id == 0 {
		id = o.Comment
	}
	if id == 0 {
		// Recover a successful POST whose response or receipt write was lost.
		for page := 1; ; page++ {
			var comments []comment
			path := fmt.Sprintf("%s/issues/%d/comments?per_page=100&page=%d", base, o.Entry, page)
			if err := p.api("GET", path, nil, &comments); err != nil {
				return err
			}
			for _, c := range comments {
				if strings.Contains(c.Body, line) && c.IssueURL == issueURL {
					id = c.ID
					break
				}
			}
			if id != 0 || len(comments) < 100 {
				break
			}
		}
	}
	var saved comment
	if id == 0 {
		if err := p.api("POST", fmt.Sprintf("%s/issues/%d/comments", base, o.Entry),
			map[string]string{"body": "## Chronicle banner\n\n" + line}, &saved); err != nil {
			return err
		}
		id = saved.ID
	} else {
		path := fmt.Sprintf("%s/issues/comments/%d", base, id)
		if err := p.api("GET", path, nil, &saved); err != nil {
			return err
		}
		if saved.IssueURL != issueURL {
			return errors.New("comment does not belong to the requested chronicle entry")
		}
		if !strings.Contains(saved.Body, line) {
			if err := p.api("PATCH", path, map[string]string{"body": strings.TrimRight(saved.Body, "\r\n") + "\n\n" + line}, &saved); err != nil {
				return err
			}
		}
	}
	if id <= 0 {
		return errors.New("body save returned no comment ID")
	}
	if err := p.api("GET", fmt.Sprintf("%s/issues/comments/%d", base, id), nil, &saved); err != nil {
		return err
	}
	if saved.ID != id || saved.IssueURL != issueURL || !strings.Contains(saved.Body, line) {
		return errors.New("saved entry body does not contain the banner markdown")
	}
	r.Comment = id
	return nil
}

func (p publisher) verify(asset, mime string) error {
	for attempt := 0; attempt < 4; attempt++ {
		req, _ := http.NewRequest("GET", asset, nil)
		req.Header.Set("Authorization", "Bearer "+p.token)
		res, err := p.client.Do(req)
		if err != nil {
			return errors.New("attachment resolution request failed")
		}
		res.Body.Close()
		if res.StatusCode == 404 && attempt < 3 {
			p.pause()
			continue
		}
		if res.StatusCode != http.StatusFound {
			return fmt.Errorf("attachment resolution returned HTTP %d, expected 302", res.StatusCode)
		}
		u, err := url.Parse(res.Header.Get("Location"))
		if err != nil || u.Scheme != "https" || u.User != nil ||
			!strings.HasSuffix(u.Hostname(), ".amazonaws.com") ||
			!(strings.HasPrefix(u.Hostname(), "s3.") || strings.Contains(u.Hostname(), ".s3.")) ||
			(u.Query().Get("X-Amz-Signature") == "" && u.Query().Get("Signature") == "") {
			return errors.New("attachment did not redirect to signed S3")
		}
		// S3 gets its signed query, never the GitHub Bearer credential.
		req, _ = http.NewRequest("GET", u.String(), nil)
		object, err := p.client.Do(req)
		if err != nil {
			return errors.New("signed image request failed")
		}
		sample, readErr := io.ReadAll(io.LimitReader(object.Body, 512))
		object.Body.Close()
		contentType := strings.Split(object.Header.Get("Content-Type"), ";")[0]
		if object.StatusCode != 200 || readErr != nil || contentType != mime || http.DetectContentType(sample) != mime {
			return fmt.Errorf("signed object is not the uploaded image type (HTTP %d)", object.StatusCode)
		}
		return nil
	}
	return errors.New("attachment did not resolve")
}
