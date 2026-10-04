// Copyright 2026 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package images

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// dockerfilePlatforms is the buildx --platform value: the install's
// KO_DEFAULTPLATFORMS, so the image lands on the same nodes as the ko images,
// or linux/amd64 when unset.
func dockerfilePlatforms(koDefaultPlatforms string) string {
	if koDefaultPlatforms == "" {
		return "linux/amd64"
	}
	return koDefaultPlatforms
}

// BuildDockerfileImage builds a Dockerfile-based image from contextPath, pushes
// it to dockerRepo/<imageName>, and returns the digest-pinned reference.
//
// The image is tagged with the build time only to give buildx a stable name to
// push to; the returned reference always uses the digest, so a stale tag can
// never be resolved by accident.
func BuildDockerfileImage(ctx context.Context, rootDir, dockerRepo, imageName, contextPath, koDefaultPlatforms string) (string, error) {
	repo := strings.TrimSuffix(dockerRepo, "/") + "/" + imageName
	stageTag := fmt.Sprintf("%s:build-%d", repo, time.Now().Unix())
	if os.Getenv("ATE_CONTAINER_BUILDER") == "podman" {
		return buildPodmanImage(ctx, rootDir, repo, stageTag, contextPath, dockerfilePlatforms(koDefaultPlatforms))
	}

	build := exec.CommandContext(ctx, "docker", "buildx", "build",
		"--platform="+dockerfilePlatforms(koDefaultPlatforms),
		"--push",
		"-t", stageTag,
	)
	build.Args = append(build.Args, additionalBuildCAArgs()...)
	build.Args = append(build.Args, contextPath)
	build.Dir = rootDir
	// The shell version sent build output to stderr so it could capture the
	// image reference on stdout; keeping that split makes the two behave the
	// same under CI log capture.
	build.Stdout = os.Stderr
	build.Stderr = os.Stderr
	if err := build.Run(); err != nil {
		return "", fmt.Errorf("while building the %s image: %w", imageName, err)
	}

	inspect := exec.CommandContext(ctx, "docker", "buildx", "imagetools", "inspect",
		stageTag, "--format", "{{json .}}")
	inspect.Dir = rootDir
	inspect.Stderr = os.Stderr
	var out bytes.Buffer
	inspect.Stdout = &out
	if err := inspect.Run(); err != nil {
		return "", fmt.Errorf("while inspecting %s: %w", stageTag, err)
	}

	var inspected struct {
		Manifest struct {
			Digest string `json:"digest"`
		} `json:"manifest"`
	}
	if err := json.Unmarshal(out.Bytes(), &inspected); err != nil {
		return "", fmt.Errorf("while parsing the image manifest of %s: %w", stageTag, err)
	}
	if inspected.Manifest.Digest == "" {
		return "", fmt.Errorf("failed to resolve the image digest from %s", stageTag)
	}
	return repo + "@" + inspected.Manifest.Digest, nil
}

func buildPodmanImage(ctx context.Context, rootDir, repo, stageTag, contextPath, platforms string) (string, error) {
	buildCommand, buildArgs := podmanBuildCommand(rootDir, stageTag, contextPath, platforms, os.Getenv("ATE_BUILD_CA_CERT_FILE"))
	build := exec.CommandContext(ctx, buildCommand, buildArgs...)
	build.Dir = rootDir
	build.Stdout = os.Stderr
	build.Stderr = os.Stderr
	if err := build.Run(); err != nil {
		return "", fmt.Errorf("while building the Envoy dataplane image with Podman: %w", err)
	}

	digestFile, err := os.CreateTemp("", "ate-image-digest-*")
	if err != nil {
		return "", fmt.Errorf("creating Podman digest file: %w", err)
	}
	digestPath := digestFile.Name()
	if err := digestFile.Close(); err != nil {
		_ = os.Remove(digestPath)
		return "", fmt.Errorf("closing Podman digest file: %w", err)
	}
	defer os.Remove(digestPath)

	pushArgs := podmanPushArgs(stageTag, digestPath, strings.HasPrefix(repo, "localhost:") || strings.HasPrefix(repo, "127."))
	push := exec.CommandContext(ctx, "podman", pushArgs...)
	push.Dir = rootDir
	push.Stdout = os.Stderr
	push.Stderr = os.Stderr
	if err := push.Run(); err != nil {
		return "", fmt.Errorf("while pushing the Envoy dataplane image with Podman: %w", err)
	}

	digest, err := os.ReadFile(filepath.Clean(digestPath))
	if err != nil {
		return "", fmt.Errorf("reading Podman image digest: %w", err)
	}
	digestString, err := parsePodmanDigest(string(digest))
	if err != nil {
		return "", err
	}
	return repo + "@" + digestString, nil
}

func podmanBuildArgs(stageTag, contextPath, platforms string) []string {
	args := []string{"build", "--platform=" + platforms, "--format=docker", "-t", stageTag}
	args = append(args, additionalBuildCAArgs()...)
	return append(args, contextPath)
}

func podmanBuildCommand(rootDir, stageTag, contextPath, platforms, caFile string) (string, []string) {
	args := podmanBuildArgs(stageTag, contextPath, platforms)
	if runtime.GOOS != "windows" || caFile == "" {
		return "podman", args
	}
	remoteArgs := []string{"machine", "ssh", "--", "podman"}
	remoteArgs = append(remoteArgs, args...)
	for i, arg := range remoteArgs {
		if strings.HasPrefix(arg, "--secret=id=ate-build-ca,src=") {
			remoteArgs[i] = "--secret=id=ate-build-ca,src=" + podmanMachinePath(strings.TrimPrefix(arg, "--secret=id=ate-build-ca,src="))
		}
	}
	buildContext := contextPath
	if !filepath.IsAbs(buildContext) {
		buildContext = filepath.Join(rootDir, buildContext)
	}
	remoteArgs[len(remoteArgs)-1] = podmanMachinePath(buildContext)
	return "podman", remoteArgs
}

func podmanMachinePath(path string) string {
	path = filepath.ToSlash(path)
	if len(path) >= 2 && path[1] == ':' {
		return "/mnt/" + strings.ToLower(path[:1]) + "/" + strings.TrimLeft(path[2:], "/")
	}
	return path
}

func additionalBuildCAArgs() []string {
	path := os.Getenv("ATE_BUILD_CA_CERT_FILE")
	if path == "" {
		return nil
	}
	return []string{"--secret=id=ate-build-ca,src=" + path}
}

func podmanPushArgs(stageTag, digestPath string, insecure bool) []string {
	args := []string{"push", "--digestfile", digestPath}
	if insecure {
		args = append(args, "--tls-verify=false")
	}
	return append(args, stageTag, "docker://"+stageTag)
}

func parsePodmanDigest(value string) (string, error) {
	digest := strings.TrimSpace(value)
	if !strings.HasPrefix(digest, "sha256:") || len(digest) != len("sha256:")+64 {
		return "", fmt.Errorf("Podman returned an invalid image digest %q", digest)
	}
	return digest, nil
}
