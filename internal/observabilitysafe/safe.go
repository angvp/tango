// Package observabilitysafe isolates failures in host-supplied observability
// implementations from the application operation being observed.
package observabilitysafe

// Call invokes fn and swallows a panic raised by the observability dependency.
func Call(fn func()) {
	defer func() { _ = recover() }()
	fn()
}
