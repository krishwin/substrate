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
	"reflect"
	"strings"
	"testing"
)

func TestPodmanBuildArgs(t *testing.T) {
	t.Setenv("ATE_BUILD_CA_CERT_FILE", "")
	want := []string{"build", "--platform=linux/amd64", "--format=docker", "-t", "localhost:5001/envoy:build-1", "cmd/dataplane/envoy"}
	if got := podmanBuildArgs("localhost:5001/envoy:build-1", "cmd/dataplane/envoy", "linux/amd64"); !reflect.DeepEqual(got, want) {
		t.Fatalf("podmanBuildArgs() = %v, want %v", got, want)
	}
}

func TestPodmanPushArgs(t *testing.T) {
	for _, tc := range []struct {
		name     string
		insecure bool
		wantTLS  bool
	}{
		{name: "local registry", insecure: true, wantTLS: true},
		{name: "remote registry", insecure: false, wantTLS: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := podmanPushArgs("localhost:5001/envoy:build-1", "digest.txt", tc.insecure)
			if got[0] != "push" || got[1] != "--digestfile" || got[2] != "digest.txt" {
				t.Fatalf("podmanPushArgs() prefix = %v", got)
			}
			if gotTLS := strings.Contains(strings.Join(got, " "), "--tls-verify=false"); gotTLS != tc.wantTLS {
				t.Errorf("insecure TLS flag = %v, want %v; args %v", gotTLS, tc.wantTLS, got)
			}
			if got[len(got)-2] != "localhost:5001/envoy:build-1" || got[len(got)-1] != "docker://localhost:5001/envoy:build-1" {
				t.Errorf("podmanPushArgs() destinations = %v", got[len(got)-2:])
			}
		})
	}
}

func TestParsePodmanDigest(t *testing.T) {
	want := "sha256:" + strings.Repeat("a", 64)
	got, err := parsePodmanDigest("\n" + want + "\n")
	if err != nil {
		t.Fatalf("parsePodmanDigest() error = %v", err)
	}
	if got != want {
		t.Errorf("parsePodmanDigest() = %q, want %q", got, want)
	}
	if _, err := parsePodmanDigest("sha256:short"); err == nil {
		t.Error("parsePodmanDigest() accepted an invalid digest")
	}
}

func TestAdditionalBuildCAArgs(t *testing.T) {
	t.Setenv("ATE_BUILD_CA_CERT_FILE", "C:/certs/root-ca.pem")
	want := []string{"--secret=id=ate-build-ca,src=C:/certs/root-ca.pem"}
	if got := additionalBuildCAArgs(); !reflect.DeepEqual(got, want) {
		t.Fatalf("additionalBuildCAArgs() = %v, want %v", got, want)
	}
	t.Setenv("ATE_BUILD_CA_CERT_FILE", "")
	if got := additionalBuildCAArgs(); len(got) != 0 {
		t.Fatalf("additionalBuildCAArgs() without a CA = %v, want none", got)
	}
}

func TestPodmanMachinePath(t *testing.T) {
	if got := podmanMachinePath(`C:\Users\dev\repo\cmd\dataplane\envoy`); got != "/mnt/c/Users/dev/repo/cmd/dataplane/envoy" {
		t.Errorf("podmanMachinePath() = %q", got)
	}
}