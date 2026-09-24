//go:build windows

package cli

import "strings"

func environmentNameEqual(left, right string) bool { return strings.EqualFold(left, right) }
