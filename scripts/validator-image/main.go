// Command validator-image prepares a pinned worker for the selected local daemon.
package main

import (
	"context"
	"crypto/sha256"
	"debug/elf"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/nguyen-duc-loc/warp-plus-connector/internal/validator"
	"github.com/nguyen-duc-loc/warp-plus-connector/internal/validator/protocol"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := build(ctx, os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func build(ctx context.Context, args []string) error {
	if len(args) > 1 || (len(args) == 1 && args[0] != "amd64" && args[0] != "arm64") {
		return fmt.Errorf("usage: scripts/build-validator-image.sh [amd64|arm64]")
	}
	adapter := validator.Docker{}
	socket, err := adapter.LocalSocket(ctx)
	if err != nil {
		return fmt.Errorf("resolve local Docker endpoint: %w", err)
	}
	// Do not let a shell context override the endpoint after resolution.
	env := without(os.Environ(), "DOCKER_HOST", "DOCKER_CONTEXT", "GOOS", "GOARCH", "CGO_ENABLED")
	docker := func(args ...string) *exec.Cmd {
		cmd := exec.CommandContext(ctx, "docker", append([]string{"--host", "unix://" + socket}, args...)...)
		cmd.Env = env
		cmd.Stderr = os.Stderr
		return cmd
	}
	data, err := docker("info", "--format", "{{json .}}").Output()
	if err != nil {
		return fmt.Errorf("inspect local daemon: %w", err)
	}
	var info struct{ OSType, Architecture string }
	if json.Unmarshal(data, &info) != nil || info.OSType != "linux" {
		return fmt.Errorf("validator_platform_unsupported")
	}
	arch := info.Architecture
	switch arch {
	case "x86_64":
		arch = "amd64"
	case "aarch64":
		arch = "arm64"
	}
	if (arch != "amd64" && arch != "arm64") || (len(args) == 1 && args[0] != arch) {
		return fmt.Errorf("validator_platform_unsupported")
	}
	dir, err := os.MkdirTemp("", "wpc-validator-image.")
	if err != nil {
		return fmt.Errorf("prepare build directory: %w", err)
	}
	defer func() { _ = os.RemoveAll(dir) }()
	worker := filepath.Join(dir, "wpc-worker")
	compile := exec.CommandContext(ctx, "go", "build", "-trimpath", "-o", worker, "./cmd/wpc-worker")
	compile.Env = append(env, "GOOS=linux", "GOARCH="+arch, "CGO_ENABLED=0")
	compile.Stdout, compile.Stderr = os.Stderr, os.Stderr
	if err := compile.Run(); err != nil {
		return fmt.Errorf("build worker: %w", err)
	}
	if err := inspectWorker(worker, arch); err != nil {
		return err
	}
	workerBytes, err := os.ReadFile(worker)
	if err != nil {
		return fmt.Errorf("read worker: %w", err)
	}
	workerHash := fmt.Sprintf("%x", sha256.Sum256(workerBytes))
	definition, err := os.ReadFile("internal/validator/image/Dockerfile")
	if err != nil {
		return fmt.Errorf("read image definition: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "Dockerfile"), definition, 0600); err != nil {
		return fmt.Errorf("stage image definition: %w", err)
	}
	imageIDPath := filepath.Join(dir, "image-id")
	prepare := docker(
		"build", "--platform", "linux/"+arch, "--iidfile", imageIDPath,
		"-t", "wpc-validator:"+protocol.WARPVersion+"-"+arch, "--provenance=false", dir,
	)
	prepare.Stdout = os.Stderr
	if err := prepare.Run(); err != nil {
		return fmt.Errorf("build image: %w", err)
	}
	data, err = os.ReadFile(imageIDPath)
	if err != nil {
		return fmt.Errorf("read image identity: %w", err)
	}
	id := strings.TrimSpace(string(data))
	if !validator.ValidImage(id) {
		return fmt.Errorf("validator_image_required")
	}
	data, err = docker("image", "inspect", id).Output()
	if err != nil {
		return fmt.Errorf("inspect prepared image: %w", err)
	}
	var images []struct {
		ID               string `json:"Id"`
		Os, Architecture string
		Config           struct{ Labels map[string]string }
	}
	if json.Unmarshal(data, &images) != nil || len(images) != 1 {
		return fmt.Errorf("validator_image_unavailable")
	}
	i := images[0]
	if i.ID != id || i.Os != "linux" || i.Architecture != arch ||
		i.Config.Labels["io.wpc.validator.protocol"] != "2" ||
		i.Config.Labels["io.wpc.validator.warp"] != protocol.WARPVersion {
		return fmt.Errorf("validator_version_mismatch")
	}
	// Inspect the installed signed package and exact worker bytes without starting
	// WARP, creating a registration, mounting host files, or allowing networking.
	data, err = docker(
		"run", "--rm", "--pull=never", "--platform", "linux/"+arch, "--network", "none",
		"--read-only", "--cap-drop", "ALL", "--security-opt", "no-new-privileges",
		"--entrypoint", "/bin/sh", id, "-ec",
		`dpkg-query -W -f='${Version}\n${Architecture}\n' cloudflare-warp; sha256sum /usr/local/bin/wpc-worker`,
	).Output()
	if err != nil {
		return fmt.Errorf("inspect installed package and worker: %w", err)
	}
	expected := protocol.WARPVersion + "\n" + arch + "\n" + workerHash + "  /usr/local/bin/wpc-worker\n"
	if string(data) != expected {
		return fmt.Errorf("validator_version_mismatch")
	}
	fmt.Fprintf(os.Stderr, "prepared linux/%s WARP %s worker sha256:%s\n", arch, protocol.WARPVersion, workerHash)
	if _, err := fmt.Fprintln(os.Stdout, id); err != nil {
		return fmt.Errorf("deliver image identity: %w", err)
	}
	return nil
}

func inspectWorker(path, arch string) error {
	f, err := elf.Open(path)
	if err != nil {
		return fmt.Errorf("inspect worker ELF: %w", err)
	}
	defer func() { _ = f.Close() }()
	expected := elf.EM_AARCH64
	if arch == "amd64" {
		expected = elf.EM_X86_64
	}
	if f.Machine != expected || f.Class != elf.ELFCLASS64 {
		return fmt.Errorf("worker architecture mismatch")
	}
	for _, program := range f.Progs {
		if program.Type == elf.PT_INTERP {
			return fmt.Errorf("worker requires a dynamic interpreter")
		}
	}
	return nil
}

func without(env []string, keys ...string) []string {
	var clean []string
	for _, entry := range env {
		name, _, _ := strings.Cut(entry, "=")
		keep := true
		for _, key := range keys {
			if name == key {
				keep = false
			}
		}
		if keep {
			clean = append(clean, entry)
		}
	}
	return clean
}
