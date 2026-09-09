//go:build !darwin

package selfupdate

// signBinary is a no-op outside macOS.
func signBinary(_ string) error { return nil }
