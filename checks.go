package tango

// AppCheck is one advisory finding contributed by an App. A nil Err means
// the check passed. Named AppCheck (not Check) to avoid colliding with the
// existing package-level Check(Config) error function from Milestone 6.
type AppCheck struct {
	Description string
	Err         error
}

// Checker is an optional capability an App may implement to contribute
// advisory checks surfaced by Registry.Checks (and, in turn, `tango check`).
// Implementing Checker is never required.
type Checker interface {
	Checks() []AppCheck
}
