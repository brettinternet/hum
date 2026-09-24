//go:build !windows

package cli

func environmentNameEqual(left, right string) bool { return left == right }
