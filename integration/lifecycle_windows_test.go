package integration

import "testing"

// These are called only by Unix-only lifecycle tests, guarded by lifecycleRequireUnix.
func lifecycleKill(int) error {
	panic("Unix-only lifecycle test reached on Windows")
}

func lifecycleAssertDetachedSession(*testing.T, int) {
	panic("Unix-only lifecycle test reached on Windows")
}
