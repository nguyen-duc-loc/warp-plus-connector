// The worker is shipped only inside the isolated validator image.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/nguyen-duc-loc/warp-plus-connector/internal/validator/privatefs"
	"github.com/nguyen-duc-loc/warp-plus-connector/internal/validator/protocol"
	"github.com/nguyen-duc-loc/warp-plus-connector/internal/validator/worker"
)

func main() { os.Exit(run()) }

func run() int {
	if runtime.GOOS != "linux" || runtime.GOARCH != "arm64" || len(os.Args) != 2 {
		return fail("validator_worker_unsupported")
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	e := worker.Engine{
		Dir: "/var/lib/wpc", VendorDir: "/var/lib/cloudflare-warp", Runner: worker.ProcessRunner{}, Clock: time.Now,
	}
	if os.Args[1] == "serve" {
		return serve(ctx)
	}
	var receipt protocol.Receipt
	var err error
	switch os.Args[1] {
	case "initialize":
		var request protocol.Request
		d := json.NewDecoder(io.LimitReader(os.Stdin, 4096))
		d.DisallowUnknownFields()
		if d.Decode(&request) != nil {
			return fail("validation_invalid_input")
		}
		receipt, err = e.Initialize(request)
	case "check":
		receipt, err = e.Start(ctx)
	case "recover":
		receipt, err = e.Recover(ctx)
	case "status":
		receipt, err = e.Status()
	case "cancel":
		err = e.Cancel()
	case "ready":
		data, versionErr := exec.CommandContext(ctx, "warp-cli", "--version").Output()
		if versionErr != nil || !strings.HasPrefix(string(data), "warp-cli "+protocol.WARPVersion+"\n") {
			return fail("validator_version_mismatch")
		}
		if reply := (worker.ProcessRunner{}).Run(ctx, "status"); reply.Code != "" {
			return fail("validator_not_ready")
		}
	default:
		return fail("validator_worker_unsupported")
	}
	if err != nil {
		return fail(err.Error())
	}
	if json.NewEncoder(os.Stdout).Encode(receipt) != nil {
		return 1
	}
	return 0
}

func fail(code string) int {
	// Only fixed internal codes are accepted, never arbitrary OS or vendor errors.
	allowed := map[string]bool{
		"validator_worker_unsupported": true, "validation_invalid_input": true, "validation_cleanup_pending": true,
		"validator_state_error": true, "validator_busy": true, "validator_version_mismatch": true,
		"validator_not_ready": true,
	}
	if !allowed[code] {
		code = "validation_cleanup_pending"
	}
	_ = json.NewEncoder(os.Stderr).Encode(map[string]string{"code": code})
	return 1
}

func serve(ctx context.Context) int {
	if privatefs.Directory("/var/lib/wpc") != nil {
		return fail("validator_state_error")
	}
	f, err := os.OpenFile("/var/lib/wpc/daemon.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return fail("validator_state_error")
	}
	defer func() { _ = f.Close() }()
	cmd := exec.CommandContext(ctx, "warp-svc")
	cmd.Stdout, cmd.Stderr = f, f
	if err := cmd.Start(); err != nil {
		return fail("validator_not_ready")
	}
	if err := cmd.Wait(); err != nil && ctx.Err() == nil {
		fmt.Fprintln(os.Stderr, "validator_daemon_stopped")
		return 1
	}
	return 0
}
