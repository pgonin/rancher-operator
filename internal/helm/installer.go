/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package helm

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"helm.sh/helm/v4/pkg/action"
	"helm.sh/helm/v4/pkg/chart/loader"
	chartv2 "helm.sh/helm/v4/pkg/chart/v2"
	"helm.sh/helm/v4/pkg/kube"
	"helm.sh/helm/v4/pkg/registry"
	"helm.sh/helm/v4/pkg/release"
	releasev1 "helm.sh/helm/v4/pkg/release/v1"
	"helm.sh/helm/v4/pkg/repo/v1"
	"sigs.k8s.io/yaml"
)

const (
	defaultInstallTimeout = 10 * time.Minute
	defaultIndexTimeout   = 30 * time.Second
)

// OCICredentials carries credentials for an OCI Helm registry. Both fields
// may be empty for anonymous registries; non-empty values trigger a Login.
type OCICredentials struct {
	Username string
	Password string
}

// LoadChartFromOCI pulls a chart from an OCI Helm registry into memory and
// loads it. ociRef should be of the form "oci://host/path/chart" (with no
// trailing tag); the version is appended as a tag.
func LoadChartFromOCI(ctx context.Context, ociRef, version string, creds OCICredentials) (*chartv2.Chart, error) {
	_ = ctx // Pull does not yet accept a context; reserved for future helm releases.

	if !strings.HasPrefix(ociRef, "oci://") {
		return nil, fmt.Errorf("expected oci:// URL, got %q", ociRef)
	}
	stripped := strings.TrimPrefix(ociRef, "oci://")
	host, _, _ := strings.Cut(stripped, "/")

	client, err := registry.NewClient()
	if err != nil {
		return nil, fmt.Errorf("creating registry client: %w", err)
	}

	if creds.Username != "" || creds.Password != "" {
		if err := client.Login(host, registry.LoginOptBasicAuth(creds.Username, creds.Password)); err != nil {
			return nil, fmt.Errorf("logging into %s: %w", host, err)
		}
	}

	ref := fmt.Sprintf("%s:%s", stripped, version)
	result, err := client.Pull(ref, registry.PullOptWithChart(true))
	if err != nil {
		return nil, fmt.Errorf("pulling %s: %w", ref, err)
	}
	if result.Chart == nil || len(result.Chart.Data) == 0 {
		return nil, fmt.Errorf("pull result for %s carries no chart data", ref)
	}

	loaded, err := loader.LoadArchive(bytes.NewReader(result.Chart.Data))
	if err != nil {
		return nil, fmt.Errorf("loading chart %s: %w", ref, err)
	}
	c, ok := loaded.(*chartv2.Chart)
	if !ok {
		return nil, fmt.Errorf("chart %s is not an apiVersion v1/v2 chart (got %T)", ref, loaded)
	}
	return c, nil
}

// LoadChartFromRepo fetches the chart matching name@version from a Helm
// repository URL into memory, then loads it. The repository URL must point
// at the root of the repo (i.e. the parent of index.yaml).
func LoadChartFromRepo(ctx context.Context, repoURL, name, version string) (*chartv2.Chart, error) {
	chartURL, err := resolveChartURL(ctx, repoURL, name, version)
	if err != nil {
		return nil, err
	}

	body, err := httpGet(ctx, chartURL)
	if err != nil {
		return nil, fmt.Errorf("downloading chart %s@%s: %w", name, version, err)
	}

	loaded, err := loader.LoadArchive(bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("loading chart %s@%s: %w", name, version, err)
	}
	c, ok := loaded.(*chartv2.Chart)
	if !ok {
		return nil, fmt.Errorf("chart %s@%s is not an apiVersion v1/v2 chart (got %T)", name, version, loaded)
	}
	return c, nil
}

// resolveChartURL fetches the repository index, locates the chart entry that
// matches the requested name and version, and resolves any relative URL it
// carries against the repository URL.
func resolveChartURL(ctx context.Context, repoURL, name, version string) (string, error) {
	base, err := url.Parse(strings.TrimRight(repoURL, "/") + "/")
	if err != nil {
		return "", fmt.Errorf("parsing repo URL %q: %w", repoURL, err)
	}
	indexURL := base.JoinPath("index.yaml").String()

	idxBytes, err := httpGet(ctx, indexURL)
	if err != nil {
		return "", fmt.Errorf("fetching %s: %w", indexURL, err)
	}

	var idx repo.IndexFile
	if err := yaml.Unmarshal(idxBytes, &idx); err != nil {
		return "", fmt.Errorf("parsing index.yaml: %w", err)
	}
	entry, err := idx.Get(name, version)
	if err != nil {
		return "", fmt.Errorf("chart %s@%s not found in %s: %w", name, version, repoURL, err)
	}
	if len(entry.URLs) == 0 {
		return "", fmt.Errorf("chart %s@%s has no download URLs in index", name, version)
	}

	raw := entry.URLs[0]
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("parsing chart URL %q: %w", raw, err)
	}
	if !parsed.IsAbs() {
		parsed = base.ResolveReference(parsed)
	}
	return parsed.String(), nil
}

func httpGet(ctx context.Context, target string) ([]byte, error) {
	reqCtx, cancel := context.WithTimeout(ctx, defaultIndexTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, target, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: status %d", target, resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

// InstallOrUpgradeOptions describes a single release reconciliation.
type InstallOrUpgradeOptions struct {
	ReleaseName     string
	Namespace       string
	CreateNamespace bool
	Chart           *chartv2.Chart
	Values          map[string]any
	Timeout         time.Duration
}

// InstallOrUpgrade installs the release if it does not yet exist, otherwise
// upgrades it in place. The returned release reflects the final desired state.
func InstallOrUpgrade(ctx context.Context, getter *KubeconfigRESTClientGetter, opts InstallOrUpgradeOptions) (*releasev1.Release, error) {
	if opts.Chart == nil {
		return nil, errors.New("chart must not be nil")
	}
	if opts.Timeout == 0 {
		opts.Timeout = defaultInstallTimeout
	}

	cfg := new(action.Configuration)
	if err := cfg.Init(getter, opts.Namespace, "secrets"); err != nil {
		return nil, fmt.Errorf("initialising helm configuration: %w", err)
	}

	_, err := action.NewGet(cfg).Run(opts.ReleaseName)
	switch {
	case err == nil:
		return runUpgrade(ctx, cfg, opts)
	case isReleaseNotFound(err):
		return runInstall(ctx, cfg, opts)
	default:
		return nil, fmt.Errorf("looking up release %q: %w", opts.ReleaseName, err)
	}
}

func runInstall(ctx context.Context, cfg *action.Configuration, opts InstallOrUpgradeOptions) (*releasev1.Release, error) {
	install := action.NewInstall(cfg)
	install.ReleaseName = opts.ReleaseName
	install.Namespace = opts.Namespace
	install.CreateNamespace = opts.CreateNamespace
	install.Timeout = opts.Timeout
	install.WaitStrategy = kube.StatusWatcherStrategy
	rel, err := install.RunWithContext(ctx, opts.Chart, opts.Values)
	if err != nil {
		return nil, fmt.Errorf("installing release %q: %w", opts.ReleaseName, err)
	}
	return asReleaseV1(rel)
}

func runUpgrade(ctx context.Context, cfg *action.Configuration, opts InstallOrUpgradeOptions) (*releasev1.Release, error) {
	upgrade := action.NewUpgrade(cfg)
	upgrade.Namespace = opts.Namespace
	upgrade.Timeout = opts.Timeout
	upgrade.WaitStrategy = kube.StatusWatcherStrategy
	rel, err := upgrade.RunWithContext(ctx, opts.ReleaseName, opts.Chart, opts.Values)
	if err != nil {
		return nil, fmt.Errorf("upgrading release %q: %w", opts.ReleaseName, err)
	}
	return asReleaseV1(rel)
}

func asReleaseV1(r release.Releaser) (*releasev1.Release, error) {
	rel, ok := r.(*releasev1.Release)
	if !ok {
		return nil, fmt.Errorf("unexpected release type %T", r)
	}
	return rel, nil
}

func isReleaseNotFound(err error) bool {
	if err == nil {
		return false
	}
	// The Helm storage layer returns a sentinel "release: not found" error.
	return strings.Contains(err.Error(), "release: not found")
}
