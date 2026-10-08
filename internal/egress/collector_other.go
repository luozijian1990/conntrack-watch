//go:build !linux

package egress

import (
	"context"
	"fmt"
	"log/slog"
)

func Run(_ context.Context, _ Config, _ *Metrics, _ *Output, _ *slog.Logger) error {
	return fmt.Errorf("eBPF collection requires Linux; configuration validation and unit tests are available on this platform")
}
