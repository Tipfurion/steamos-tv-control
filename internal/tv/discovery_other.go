//go:build !darwin

package tv

import (
	"context"
	"errors"
	atv "github.com/jkiddo/atvremote/pkg/v2/remote"
)

func discoverNative(context.Context) ([]atv.DiscoveredDevice, error) {
	return nil, errors.New("native discovery unavailable")
}
