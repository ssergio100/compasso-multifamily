//go:build windows
// +build windows

package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/ssergio100/compasso/agent/platform/windows/companion"
	"github.com/ssergio100/compasso/agent/platform/windows/ipc"
)

func main() {
	pipeName := flag.String("pipe-name", "CompassoAgent", "logical local service pipe name")
	timeout := flag.Duration("connect-timeout", 30*time.Second, "maximum time waiting for the service pipe")
	flag.Parse()

	pipePath, err := ipc.PipePath(*pipeName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "resolve Windows companion pipe: %v\n", err)
		os.Exit(1)
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	if err := companion.Run(ctx, pipePath); err != nil {
		fmt.Fprintf(os.Stderr, "run Windows companion: %v\n", err)
		os.Exit(1)
	}
}
