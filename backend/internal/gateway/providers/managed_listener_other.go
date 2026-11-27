//go:build !linux && !darwin && !windows

package providers

import (
	"context"
	"errors"
	"os"
)

func lockManagedRequest(*os.File) error                         { return errors.New("unsupported platform") }
func managedPeerMatches(context.Context, uint32, int, int) bool { return false }

func managedGroup(uint32) uint32 { return 0 }
