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

// Package helm provides a thin wrapper around helm.sh/helm/v4 sufficient to
// install and upgrade Helm releases on a remote target cluster identified by
// a kubeconfig blob (rather than by ambient in-cluster credentials).
package helm

import (
	"fmt"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/discovery/cached/memory"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/restmapper"
	"k8s.io/client-go/tools/clientcmd"
)

// KubeconfigRESTClientGetter implements genericclioptions.RESTClientGetter from
// a kubeconfig byte slice. Helm's action.Configuration consumes this interface,
// so wrapping a kubeconfig in one lets us drive Helm against any cluster
// without writing the kubeconfig to disk.
type KubeconfigRESTClientGetter struct {
	clientConfig clientcmd.ClientConfig
	parseErr     error
}

var _ genericclioptions.RESTClientGetter = (*KubeconfigRESTClientGetter)(nil)

// NewKubeconfigRESTClientGetter wraps a kubeconfig blob into a RESTClientGetter.
// The kubeconfig is parsed eagerly; subsequent method calls return the cached
// error on every interface entry-point so the consumer sees a consistent
// failure mode.
func NewKubeconfigRESTClientGetter(kubeconfig []byte) *KubeconfigRESTClientGetter {
	cfg, err := clientcmd.NewClientConfigFromBytes(kubeconfig)
	return &KubeconfigRESTClientGetter{clientConfig: cfg, parseErr: err}
}

func (g *KubeconfigRESTClientGetter) ToRESTConfig() (*rest.Config, error) {
	if g.parseErr != nil {
		return nil, fmt.Errorf("parsing kubeconfig: %w", g.parseErr)
	}
	return g.clientConfig.ClientConfig()
}

func (g *KubeconfigRESTClientGetter) ToDiscoveryClient() (discovery.CachedDiscoveryInterface, error) {
	cfg, err := g.ToRESTConfig()
	if err != nil {
		return nil, err
	}
	dc, err := discovery.NewDiscoveryClientForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("building discovery client: %w", err)
	}
	return memory.NewMemCacheClient(dc), nil
}

func (g *KubeconfigRESTClientGetter) ToRESTMapper() (meta.RESTMapper, error) {
	dc, err := g.ToDiscoveryClient()
	if err != nil {
		return nil, err
	}
	return restmapper.NewDeferredDiscoveryRESTMapper(dc), nil
}

func (g *KubeconfigRESTClientGetter) ToRawKubeConfigLoader() clientcmd.ClientConfig {
	return g.clientConfig
}
