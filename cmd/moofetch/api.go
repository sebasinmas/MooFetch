package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/sebasinmas/MooFetch/internal/plugins/moodle"
	"github.com/sebasinmas/MooFetch/internal/tui"
)

// Valid values of --auth.
const (
	authModeAPI    = "api"
	authModeCookie = "cookie"
)

var flagAuth = authModeAPI

// validateAuthFlag rejects values other than api or cookie.
func validateAuthFlag() error {
	switch flagAuth {
	case authModeAPI, authModeCookie:
		return nil
	}
	return fmt.Errorf("valor de --auth inválido %q: usa api o cookie", flagAuth)
}

// newMobileAPI builds the mobile-API client of one campus (siteURL is
// "https://<host>"). The composition root wires the real client here; tests
// replace it with fakes.
var newMobileAPI = func(siteURL string) (moodle.MobileAPI, error) {
	moodle.UserAgentVersion = Version
	return moodle.NewAPIClient(siteURL)
}

// apiAdapter adapts moodle.MobileAPI to the function types the wizard
// receives. It keeps one client per campus. It never logs or stores the
// password or the token.
type apiAdapter struct {
	mu      sync.Mutex
	clients map[string]moodle.MobileAPI
}

func newAPIAdapter() *apiAdapter {
	return &apiAdapter{clients: map[string]moodle.MobileAPI{}}
}

func (a *apiAdapter) client(campus string) (moodle.MobileAPI, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if c, ok := a.clients[campus]; ok {
		return c, nil
	}
	c, err := newMobileAPI("https://" + campus)
	if err != nil {
		return nil, err
	}
	a.clients[campus] = c
	return c, nil
}

// mapAPIError translates the plugin's "no mobile API" error to the wizard's.
// Errors wrapping domain.ErrAuthenticationFailed pass through unchanged.
func mapAPIError(err error) error {
	if errors.Is(err, moodle.ErrAPIUnavailable) {
		return tui.ErrAPIUnavailable
	}
	return err
}

// Login exchanges the credentials for a token.
func (a *apiAdapter) Login(ctx context.Context, campus, user, pass string) (string, error) {
	c, err := a.client(campus)
	if err != nil {
		return "", err
	}
	token, err := c.Login(ctx, user, pass)
	if err != nil {
		return "", mapAPIError(err)
	}
	return token, nil
}

// Courses lists the user's courses (site info first, for the user id).
func (a *apiAdapter) Courses(ctx context.Context, campus, token string) ([]tui.Course, error) {
	c, err := a.client(campus)
	if err != nil {
		return nil, err
	}
	info, err := c.SiteInfo(ctx, token)
	if err != nil {
		return nil, mapAPIError(err)
	}
	courses, err := c.Courses(ctx, token, info.UserID)
	if err != nil {
		return nil, mapAPIError(err)
	}
	out := make([]tui.Course, len(courses))
	for i, co := range courses {
		name := strings.TrimSpace(co.FullName)
		if name == "" {
			name = co.ShortName
		}
		out[i] = tui.Course{ID: co.ID, Name: name}
	}
	return out, nil
}

// Contents lists the sections and files of one course.
func (a *apiAdapter) Contents(ctx context.Context, campus, token string, courseID int) ([]tui.Section, error) {
	c, err := a.client(campus)
	if err != nil {
		return nil, err
	}
	sections, err := c.Contents(ctx, token, courseID)
	if err != nil {
		return nil, mapAPIError(err)
	}
	out := make([]tui.Section, len(sections))
	for i, s := range sections {
		files := make([]tui.FileItem, len(s.Files))
		for j, f := range s.Files {
			name := f.Name
			if strings.TrimSpace(name) == "" {
				name = f.FileName
			}
			files[j] = tui.FileItem{Name: name, URL: f.URL, SubDir: f.SubDir, Size: f.Size}
		}
		out[i] = tui.Section{ID: s.ID, Number: s.Number, Name: s.Name, Files: files}
	}
	return out, nil
}
