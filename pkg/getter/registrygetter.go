/*
Copyright The Helm Authors.
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

package getter

import (
	"bytes"
	"helm.sh/helm/v3/internal/experimental/registry"
	"net/url"
)

// TODO: remove this file and just put everything into the experimental file

type RegistryGetter struct {
	g *registry.Getter
}

func NewRegistryGetter(c *registry.Client) *RegistryGetter {
	return &RegistryGetter{g: &registry.Getter{Client: c}}
}

func NewRegistryGetterProvider(c *registry.Client) Provider {
	return Provider{
		Schemes: []string{"oci"},
		New: func(options ...Option) (getter Getter, e error) {
			return NewRegistryGetter(c), nil
		},
	}
}

func (rg *RegistryGetter) Get(href string, options ...Option) (*bytes.Buffer, error) {
	return rg.g.Get(href)
}

func (rg *RegistryGetter) Filename(u *url.URL, version string) string {
	return rg.g.Filename(u, version)
}
