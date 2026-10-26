package auth

import "context"

// Outcome is what happened when MooFetch tried to read one browser cookie
// store. It never carries cookie values.
type Outcome string

// Possible outcomes of reading one store.
const (
	// OutcomeFound: a matching session cookie was read.
	OutcomeFound Outcome = "encontrada"
	// OutcomeNoCookie: the store was read but has no cookie for the campus
	// (usually the user is not signed in with that browser).
	OutcomeNoCookie Outcome = "sin-cookie"
	// OutcomeLocked: the file is in use by the browser and could not be read.
	OutcomeLocked Outcome = "bloqueado"
	// OutcomeEncrypted: the browser protects its cookies in a way MooFetch
	// cannot undo (for example Chrome/Edge "v20" app-bound encryption on Windows).
	OutcomeEncrypted Outcome = "cifrado"
	// OutcomeUnreadable: any other read or parse error.
	OutcomeUnreadable Outcome = "ilegible"
	// OutcomeMissing: the browser (or its profile or file) is not installed.
	OutcomeMissing Outcome = "ausente"
)

// Strategy is how a store was read.
type Strategy string

// Reading strategies, tried in this order.
const (
	// StrategyShared opens the file allowing other processes to keep using it
	// and reads it into memory.
	StrategyShared Strategy = "lectura compartida"
	// StrategyCopy copies the file to a private temporary folder, reads the
	// copy and deletes it. Fallback only.
	StrategyCopy Strategy = "copia temporal"
	// StrategyNone: nothing worked (or nothing was tried).
	StrategyNone Strategy = "-"
)

// StoreReport describes the result for one browser profile.
type StoreReport struct {
	Browser  string
	Profile  string
	Outcome  Outcome
	Strategy Strategy
	// Err is the underlying error for diagnostics. It may contain file paths
	// but never cookie values; the CLI shows it only in `doctor` and --log.
	Err error
}

// Diagnoser reports, per browser store, what happened when looking for the
// campus session cookie. Used by `moofetch doctor`.
type Diagnoser interface {
	Diagnose(ctx context.Context, domain string) []StoreReport
}
