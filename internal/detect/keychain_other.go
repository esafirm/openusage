//go:build !darwin

package detect

// detectMacOSKeychainCredentials is a no-op on Linux. Linux credential stores
// are not consulted by the AI CLIs this build supports.
func detectMacOSKeychainCredentials(_ *Result) {}
