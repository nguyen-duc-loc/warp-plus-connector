package worker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

type boundedBuffer struct {
	bytes.Buffer
	overflow bool
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	if b.Len()+len(p) > 256*1024 {
		b.overflow = true
		return 0, errors.New("bounded output exceeded")
	}
	return b.Buffer.Write(p)
}

// ProcessRunner invokes the official CLI and strips raw process errors.
type ProcessRunner struct{}

// Run bounds one vendor command and returns either JSON data or a stable code.
func (ProcessRunner) Run(parent context.Context, args ...string) Reply {
	if len(args) == 2 && args[0] == "registration" && args[1] == "creation-status" {
		f, err := os.Open("/var/lib/wpc/daemon.log")
		if err != nil {
			return Reply{Code: "validation_provider_error"}
		}
		data, err := io.ReadAll(io.LimitReader(f, 8*1024*1024+1))
		_ = f.Close()
		if err == nil && len(data) <= 8*1024*1024 && registrationRejected(data) {
			return Reply{Code: "registration_rate_limited"}
		}
		return Reply{Code: "validation_provider_error"}
	}
	ctx, cancel := context.WithTimeout(parent, 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "warp-cli", append([]string{"--accept-tos", "--json"}, args...)...)
	cmd.WaitDelay = 2 * time.Second
	var out, diagnostic boundedBuffer
	cmd.Stdout, cmd.Stderr = &out, &diagnostic
	err := cmd.Run()
	if ctx.Err() != nil {
		return Reply{Code: "validation_timeout"}
	}
	if out.overflow || diagnostic.overflow {
		return Reply{Code: "validation_provider_error"}
	}
	if err != nil {
		var value struct {
			Code string `json:"code"`
		}
		data := diagnostic.Bytes()
		if len(data) == 0 {
			data = out.Bytes()
		}
		if json.Unmarshal(data, &value) == nil && value.Code != "" {
			return Reply{Code: value.Code}
		}
		return Reply{Code: "validation_provider_error"}
	}
	if !json.Valid(out.Bytes()) {
		return Reply{Code: "validation_provider_error"}
	}
	mutation := len(args) == 1 && args[0] == "disconnect"
	if len(args) >= 2 && args[0] == "registration" && (args[1] == "new" || args[1] == "license" ||
		args[1] == "delete") {
		mutation = true
	}
	if mutation && !acknowledged(out.Bytes()) {
		return Reply{Code: "validation_provider_error"}
	}
	return Reply{Data: out.Bytes()}
}

var ansiPattern = regexp.MustCompile(`\x1b\[[0-9;]*m`)
var registrationRequestPattern = regexp.MustCompile(`request_id=([a-f0-9-]{36}) request=new_registration`)

// The pinned CLI reports registration throttling only in the private daemon log.
// Require one exact request and both its HTTP rejection and terminal no-retry result.
func registrationRejected(data []byte) bool {
	requests := map[string]bool{}
	rejected, terminal := "", ""
	failures, completions := 0, 0
	for _, line := range strings.Split(ansiPattern.ReplaceAllString(string(data), ""), "\n") {
		match := registrationRequestPattern.FindStringSubmatch(line)
		if len(match) != 2 {
			continue
		}
		id := match[1]
		requests[id] = true
		if strings.Contains(line, "Failed API Request") {
			failures++
			if failures != 1 || completions != 0 || !strings.Contains(line, "ApiFailure { code: 429,") {
				return false
			}
			rejected = id
		}
		if strings.Contains(line, "Request completed") {
			completions++
			if completions != 1 || failures != 1 || !strings.Contains(
				line,
				"result=Err(MaxRateLimitAttemptsReached(0))",
			) || !strings.Contains(line, "is_rate_limited: true") {
				return false
			}
			terminal = id
		}
	}
	return len(requests) == 1 && failures == 1 && completions == 1 && rejected != "" && rejected == terminal
}

func acknowledged(data []byte) bool {
	var response struct {
		Status string `json:"status"`
	}
	return json.Unmarshal(data, &response) == nil && response.Status == "Success"
}
