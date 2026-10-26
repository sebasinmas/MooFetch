package moodle

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/sebasinmas/MooFetch/internal/domain"
)

// MobileAPI is the part of Moodle's mobile web service (the one the official
// app uses) that MooFetch needs. See docs/adr/006-autenticacion-api-movil.md.
//
// Tokens are secrets: implementations must never log them or put them in
// errors, and the password given to Login must not be kept.
type MobileAPI interface {
	// Login exchanges a username and password for a token (login/token.php).
	Login(ctx context.Context, username, password string) (token string, err error)
	// SiteInfo validates the token and returns the user id and site details.
	SiteInfo(ctx context.Context, token string) (SiteInfo, error)
	// Courses lists the courses the user is enrolled in.
	Courses(ctx context.Context, token string, userID int) ([]APICourse, error)
	// Contents returns the sections and downloadable files of one course.
	Contents(ctx context.Context, token string, courseID int) ([]APISection, error)
}

// SiteInfo is the subset of core_webservice_get_site_info MooFetch uses.
type SiteInfo struct {
	UserID   int
	SiteName string
	Release  string
	// Functions are the web-service functions available to the token.
	Functions map[string]bool
}

// APICourse is an enrolled course.
type APICourse struct {
	ID        int
	FullName  string
	ShortName string
}

// APISection is a course section with its downloadable files.
type APISection struct {
	ID     int
	Number int // position in the course, starting at 0
	Name   string
	Files  []APIFile
	// OtherModules counts visible activities of a type MooFetch does not
	// download (pages, assignments...) that do carry files.
	OtherModules int
}

// APIFile is one downloadable file of a section.
type APIFile struct {
	// Name is the title to show: the activity name, or the file name for
	// files inside a folder.
	Name string
	// FileName is the real file name on the server.
	FileName string
	// SubDir is "" for resources. For files of a "folder" activity it is the
	// folder's module name followed by the file's filepath without the
	// leading/trailing slashes: filepath "/" gives "<module>", "/a/b/" gives
	// "<module>/a/b". A slash or backslash inside the module name becomes
	// "-" and "." / ".." segments are dropped.
	SubDir string
	// URL is the download URL WITHOUT the token. The token travels in
	// domain.Task.Token.
	URL      string
	Size     int64
	Modified time.Time
	// Module is the Moodle module type ("resource" or "folder").
	Module string
}

var (
	// ErrInvalidCredentials means the campus rejected the username/password.
	ErrInvalidCredentials = fmt.Errorf("%w: usuario o contraseña incorrectos", domain.ErrAuthenticationFailed)
	// ErrInvalidToken means the token is invalid or expired.
	ErrInvalidToken = fmt.Errorf("%w: token inválido o expirado", domain.ErrAuthenticationFailed)
	// ErrAPIUnavailable means the campus does not offer the mobile web
	// service (or it is disabled for this account).
	ErrAPIUnavailable = errors.New("el campus no ofrece la API móvil de Moodle para esta cuenta")
	// ErrInsecureURL means the site URL is not https.
	ErrInsecureURL = errors.New("la URL del campus debe usar https")
)
