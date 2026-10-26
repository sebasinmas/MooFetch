package moodle

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/sebasinmas/MooFetch/internal/logger"
)

const (
	maxAPIBody   = 8 << 20 // cap on every web-service response
	maxLoginBody = 1 << 20 // cap on the login/token.php response

	loginPath = "/login/token.php"
	wsPath    = "/webservice/rest/server.php"

	mobileService = "moodle_mobile_app"
)

// APIOption configures an APIClient.
type APIOption func(*APIClient)

// WithAPIHTTPClient sets the HTTP client (tests use httptest's TLS client).
// The client is copied and redirects are disabled on the copy: a 307/308
// would otherwise resend the password or token to wherever it points.
func WithAPIHTTPClient(c *http.Client) APIOption {
	return func(a *APIClient) {
		if c != nil {
			cp := *c
			a.client = &cp
		}
	}
}

// WithAPIDelay sets the pause between two consecutive requests (default
// DefaultScanDelay). A value <= 0 disables the pause.
func WithAPIDelay(d time.Duration) APIOption {
	return func(a *APIClient) { a.delay = max(d, 0) }
}

// WithAPISleep injects the function that waits for d or until ctx is done.
func WithAPISleep(f func(ctx context.Context, d time.Duration) error) APIOption {
	return func(a *APIClient) {
		if f != nil {
			a.sleep = f
		}
	}
}

// WithAPIUserAgent overrides the identifiable default User-Agent.
func WithAPIUserAgent(ua string) APIOption {
	return func(a *APIClient) {
		if strings.TrimSpace(ua) != "" {
			a.userAgent = ua
		}
	}
}

// WithAPILogger sets an optional debug logger. Neither the token nor the
// password are ever passed to it.
func WithAPILogger(l *logger.Logger) APIOption {
	return func(a *APIClient) { a.log = l }
}

// APIClient implements MobileAPI against a Moodle site over HTTPS. Requests
// are strictly sequential and spaced by a configurable pause.
type APIClient struct {
	base      *url.URL // https://host[/prefix], no trailing slash
	client    *http.Client
	delay     time.Duration
	sleep     func(ctx context.Context, d time.Duration) error
	userAgent string
	log       *logger.Logger

	sem  chan struct{} // capacity 1: serialises requests
	used bool          // a request was already made (pacing skips the first)
}

var _ MobileAPI = (*APIClient)(nil)

// NewAPIClient validates siteURL (https only) and builds the client.
func NewAPIClient(siteURL string, opts ...APIOption) (*APIClient, error) {
	base, err := normaliseSiteURL(siteURL)
	if err != nil {
		return nil, err
	}
	a := &APIClient{
		base:      base,
		client:    &http.Client{Timeout: 60 * time.Second},
		delay:     DefaultScanDelay,
		sleep:     sleepCtx,
		userAgent: fmt.Sprintf("MooFetch/%s (+%s)", UserAgentVersion, repositoryAddress),
		sem:       make(chan struct{}, 1),
	}
	for _, opt := range opts {
		opt(a)
	}
	a.client.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}
	return a, nil
}

// normaliseSiteURL keeps scheme, host and path prefix; credentials, query
// and fragment are dropped. A bare host name is taken as https.
func normaliseSiteURL(raw string) (*url.URL, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, errors.New("falta la URL del campus")
	}
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" {
		return nil, errors.New("la URL del campus no es válida")
	}
	if !strings.EqualFold(u.Scheme, "https") {
		return nil, ErrInsecureURL
	}
	return &url.URL{
		Scheme: "https",
		Host:   strings.ToLower(u.Host),
		Path:   strings.TrimRight(u.Path, "/"),
	}, nil
}

// endpoint returns the absolute URL of a Moodle script.
func (a *APIClient) endpoint(p string) string {
	u := *a.base
	u.Path += p
	return u.String()
}

// acquire waits for the single request slot and applies the pacing pause
// (none before the first request).
func (a *APIClient) acquire(ctx context.Context) error {
	select {
	case a.sem <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	if a.used && a.delay > 0 {
		if err := a.sleep(ctx, a.delay); err != nil {
			<-a.sem
			return err
		}
	}
	a.used = true
	return nil
}

func (a *APIClient) release() { <-a.sem }

// post sends a form to one of the Moodle scripts and returns the capped body.
// The error never contains the form, the response body or the request URL.
func (a *APIClient) post(ctx context.Context, p string, form url.Values, limit int64) ([]byte, error) {
	if err := a.acquire(ctx); err != nil {
		return nil, err
	}
	defer a.release()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.endpoint(p), strings.NewReader(form.Encode()))
	if err != nil {
		return nil, errors.New("no se pudo preparar la petición al campus")
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", a.userAgent)

	resp, err := a.client.Do(req)
	if err != nil {
		return nil, a.requestError(ctx, p, err)
	}
	defer func() { _ = resp.Body.Close() }()
	a.debug("moodle api response", "path", p, "status", resp.StatusCode)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("el campus respondió HTTP %d en %s", resp.StatusCode, p)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, a.requestError(ctx, p, err)
	}
	if int64(len(body)) > limit {
		return nil, fmt.Errorf("la respuesta de %s supera el límite de %d bytes", p, limit)
	}
	return body, nil
}

// requestError turns a transport error into one that carries no URL.
func (a *APIClient) requestError(ctx context.Context, p string, err error) error {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	var ue *url.Error
	if errors.As(err, &ue) {
		err = ue.Err
	}
	return fmt.Errorf("falló la petición a %s: %s", p, logger.RedactURL(err.Error()))
}

func (a *APIClient) debug(msg string, args ...any) {
	a.log.Debug(msg, args...)
}

// safeCode keeps a server-provided error code only if it looks like one, so
// a hostile or odd response cannot smuggle arbitrary text into our errors.
func safeCode(code string) string {
	if code == "" || len(code) > 64 {
		return "desconocido"
	}
	for _, r := range code {
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') && r != '_' && r != '-' && r != '.' {
			return "desconocido"
		}
	}
	return code
}

// ---------------------------------------------------------------- login

type loginResponse struct {
	Token     string `json:"token"`
	Error     string `json:"error"`
	ErrorCode string `json:"errorcode"`
}

// apiUnavailableLoginCodes are token.php errors meaning the mobile service is
// not offered (as opposed to bad credentials).
var apiUnavailableLoginCodes = map[string]bool{
	"webservicesnotenabled": true,
	"servicenotavailable":   true,
	"enablewsdescription":   true,
	"servicenotexist":       true,
	"invalidservice":        true,
	"restwsnotenabled":      true,
}

// Login exchanges username and password for a token. Neither is logged or
// kept; the token is only returned.
func (a *APIClient) Login(ctx context.Context, username, password string) (string, error) {
	form := url.Values{
		"username": {username},
		"password": {password},
		"service":  {mobileService},
	}
	a.debug("moodle api login")
	body, err := a.post(ctx, loginPath, form, maxLoginBody)
	if err != nil {
		return "", err
	}
	var r loginResponse
	if err := json.Unmarshal(body, &r); err != nil {
		return "", errors.New("el campus respondió algo que no es JSON al iniciar sesión")
	}
	if r.ErrorCode != "" || r.Error != "" {
		return "", loginError(r.ErrorCode)
	}
	if strings.TrimSpace(r.Token) == "" {
		return "", errors.New("el campus no entregó un token al iniciar sesión")
	}
	return r.Token, nil
}

func loginError(code string) error {
	switch {
	case code == "invalidlogin":
		return ErrInvalidCredentials
	case apiUnavailableLoginCodes[code]:
		return ErrAPIUnavailable
	}
	return fmt.Errorf("el campus rechazó el inicio de sesión (errorcode: %s)", safeCode(code))
}

// ------------------------------------------------------- webservice calls

type wsProbe struct {
	Exception string `json:"exception"`
	ErrorCode string `json:"errorcode"`
	Error     string `json:"error"`
}

// call invokes a web-service function. The token travels in the POST body,
// never in the URL, and the decoded result goes to out.
func (a *APIClient) call(ctx context.Context, token, fn string, params url.Values, out any) error {
	form := url.Values{
		"wstoken":            {token},
		"wsfunction":         {fn},
		"moodlewsrestformat": {"json"},
	}
	for k, v := range params {
		form[k] = v
	}
	a.debug("moodle api call", "function", fn)
	body, err := a.post(ctx, wsPath, form, maxAPIBody)
	if err != nil {
		return err
	}
	if err := wsException(fn, body); err != nil {
		return err
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("respuesta inesperada del campus en %s", fn)
	}
	return nil
}

// wsException detects a Moodle exception object (HTTP 200) in a response.
func wsException(fn string, body []byte) error {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		if len(trimmed) == 0 {
			return fmt.Errorf("respuesta vacía del campus en %s", fn)
		}
		return nil
	}
	var p wsProbe
	if err := json.Unmarshal(trimmed, &p); err != nil || (p.Exception == "" && p.ErrorCode == "") {
		return nil
	}
	switch p.ErrorCode {
	case "invalidtoken":
		return ErrInvalidToken
	case "accessexception":
		return fmt.Errorf("%w (acceso denegado a %s)", ErrInvalidToken, fn)
	case "webservicesnotenabled", "servicenotavailable", "enablewsdescription", "invalidfunction":
		return ErrAPIUnavailable
	default:
		return fmt.Errorf("el campus devolvió un error en %s (errorcode: %s)", fn, safeCode(p.ErrorCode))
	}
}

type siteInfoResponse struct {
	SiteName  string `json:"sitename"`
	UserID    int    `json:"userid"`
	Release   string `json:"release"`
	Functions []struct {
		Name string `json:"name"`
	} `json:"functions"`
}

// SiteInfo validates the token and returns the user id and site details.
func (a *APIClient) SiteInfo(ctx context.Context, token string) (SiteInfo, error) {
	var r siteInfoResponse
	if err := a.call(ctx, token, "core_webservice_get_site_info", nil, &r); err != nil {
		return SiteInfo{}, err
	}
	if r.UserID <= 0 {
		return SiteInfo{}, errors.New("el campus no informó el usuario de la sesión")
	}
	funcs := make(map[string]bool, len(r.Functions))
	for _, f := range r.Functions {
		funcs[f.Name] = true
	}
	return SiteInfo{UserID: r.UserID, SiteName: r.SiteName, Release: r.Release, Functions: funcs}, nil
}

type courseResponse struct {
	ID        int    `json:"id"`
	ShortName string `json:"shortname"`
	FullName  string `json:"fullname"`
}

// Courses lists the user's courses sorted by full name (case-insensitive)
// and de-duplicated by id.
func (a *APIClient) Courses(ctx context.Context, token string, userID int) ([]APICourse, error) {
	var raw []courseResponse
	params := url.Values{"userid": {strconv.Itoa(userID)}}
	if err := a.call(ctx, token, "core_enrol_get_users_courses", params, &raw); err != nil {
		return nil, err
	}
	seen := make(map[int]bool, len(raw))
	courses := make([]APICourse, 0, len(raw))
	for _, c := range raw {
		if c.ID <= 0 || seen[c.ID] {
			continue
		}
		seen[c.ID] = true
		name := strings.TrimSpace(c.FullName)
		if name == "" {
			name = strings.TrimSpace(c.ShortName)
		}
		courses = append(courses, APICourse{ID: c.ID, FullName: name, ShortName: strings.TrimSpace(c.ShortName)})
	}
	sort.SliceStable(courses, func(i, j int) bool {
		li, lj := sortKey(courses[i].FullName), sortKey(courses[j].FullName)
		if li != lj {
			return li < lj
		}
		return courses[i].ID < courses[j].ID
	})
	return courses, nil
}

// accentFolder maps Spanish accented letters to their plain form so that
// "Álgebra" sorts among the A's and not after "z".
var accentFolder = strings.NewReplacer(
	"á", "a", "é", "e", "í", "i", "ó", "o", "ú", "u", "ü", "u", "ñ", "n",
)

// sortKey is the case- and accent-insensitive key used to order courses.
func sortKey(s string) string {
	return accentFolder.Replace(strings.ToLower(s))
}

type contentsFile struct {
	Type         string `json:"type"`
	FileName     string `json:"filename"`
	FilePath     string `json:"filepath"`
	FileSize     int64  `json:"filesize"`
	FileURL      string `json:"fileurl"`
	TimeModified int64  `json:"timemodified"`
}

type contentsModule struct {
	Name        string         `json:"name"`
	ModName     string         `json:"modname"`
	UserVisible *bool          `json:"uservisible"`
	Contents    []contentsFile `json:"contents"`
}

type contentsSection struct {
	ID      int              `json:"id"`
	Name    string           `json:"name"`
	Section int              `json:"section"`
	Modules []contentsModule `json:"modules"`
}

// Contents returns the sections and downloadable files of a course. Sections
// keep their order, including those without files.
func (a *APIClient) Contents(ctx context.Context, token string, courseID int) ([]APISection, error) {
	var raw []contentsSection
	params := url.Values{"courseid": {strconv.Itoa(courseID)}}
	if err := a.call(ctx, token, "core_course_get_contents", params, &raw); err != nil {
		return nil, err
	}
	skipped := 0
	sections := make([]APISection, 0, len(raw))
	for _, s := range raw {
		sec := APISection{ID: s.ID, Number: s.Section, Name: strings.TrimSpace(s.Name), Files: []APIFile{}}
		for _, m := range s.Modules {
			if hasOtherModuleFiles(m) {
				sec.OtherModules++
			}
			files, n := a.moduleFiles(m)
			sec.Files = append(sec.Files, files...)
			skipped += n
		}
		sections = append(sections, sec)
	}
	if skipped > 0 {
		a.debug("moodle api: files skipped (foreign host or invalid URL)", "count", skipped)
	}
	return sections, nil
}

// hasOtherModuleFiles reports whether m is a visible activity that MooFetch
// does not download but that holds files (so the user can be told).
func hasOtherModuleFiles(m contentsModule) bool {
	if m.ModName == "resource" || m.ModName == "folder" || (m.UserVisible != nil && !*m.UserVisible) {
		return false
	}
	for _, c := range m.Contents {
		if c.Type == "file" {
			return true
		}
	}
	return false
}

// moduleFiles converts the files of a resource or folder module. The second
// result is the number of files skipped because of an unsafe URL.
func (a *APIClient) moduleFiles(m contentsModule) ([]APIFile, int) {
	if m.ModName != "resource" && m.ModName != "folder" {
		return nil, 0
	}
	if m.UserVisible != nil && !*m.UserVisible {
		return nil, 0
	}
	var files []APIFile
	skipped := 0
	for _, c := range m.Contents {
		if c.Type != "file" || strings.TrimSpace(c.FileName) == "" {
			continue
		}
		clean, ok := a.cleanFileURL(c.FileURL)
		if !ok {
			skipped++
			continue
		}
		f := APIFile{
			Name:     strings.TrimSpace(m.Name),
			FileName: c.FileName,
			URL:      clean,
			Size:     c.FileSize,
			Module:   m.ModName,
		}
		if c.TimeModified > 0 {
			f.Modified = time.Unix(c.TimeModified, 0)
		}
		if m.ModName == "folder" {
			f.Name = c.FileName
			f.SubDir = folderSubDir(m.Name, c.FilePath)
		}
		files = append(files, f)
	}
	return files, skipped
}

// folderSubDir builds "<folder name>/<filepath>" ("/" or "/a/b/" give the
// folder name or "<folder>/a/b"). Separators inside the folder name become
// "-" and "." / ".." segments are dropped, so the result cannot climb out of
// the output directory.
func folderSubDir(folderName, filePath string) string {
	name := strings.NewReplacer("/", "-", "\\", "-").Replace(strings.TrimSpace(folderName))
	parts := []string{}
	if name != "" && name != "." && name != ".." {
		parts = append(parts, name)
	}
	for _, seg := range strings.Split(strings.ReplaceAll(filePath, "\\", "/"), "/") {
		if seg = strings.TrimSpace(seg); seg != "" && seg != "." && seg != ".." {
			parts = append(parts, seg)
		}
	}
	return strings.Join(parts, "/")
}

// cleanFileURL validates that a file URL points at the campus itself (so the
// token added later can never be sent elsewhere) and removes any token from
// it. The second result is false if the URL must be skipped.
func (a *APIClient) cleanFileURL(raw string) (string, bool) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || !strings.EqualFold(u.Scheme, "https") {
		return "", false
	}
	if canonicalHost(u) != canonicalHost(a.base) {
		return "", false
	}
	u.User = nil
	u.Fragment = ""
	u.RawQuery = stripSecretParams(u.RawQuery)
	return u.String(), true
}

// canonicalHost is the lower-case host with the default https port made
// explicit, so "host" and "host:443" compare equal.
func canonicalHost(u *url.URL) string {
	port := u.Port()
	if port == "" {
		port = "443"
	}
	return strings.ToLower(u.Hostname()) + ":" + port
}

// stripSecretParams removes token and wstoken parameters from a raw query
// string without re-encoding the remaining ones.
func stripSecretParams(rawQuery string) string {
	if rawQuery == "" {
		return ""
	}
	kept := make([]string, 0, 4)
	for _, pair := range strings.Split(rawQuery, "&") {
		name := pair
		if i := strings.IndexByte(pair, '='); i >= 0 {
			name = pair[:i]
		}
		if n, err := url.QueryUnescape(name); err == nil {
			name = n
		}
		switch strings.ToLower(name) {
		case "token", "wstoken", "":
			continue
		}
		kept = append(kept, pair)
	}
	return strings.Join(kept, "&")
}
