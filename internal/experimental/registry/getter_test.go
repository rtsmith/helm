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

package registry

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	auth "github.com/deislabs/oras/pkg/auth/docker"
	"github.com/docker/distribution/configuration"
	"github.com/docker/distribution/registry"
	"github.com/stretchr/testify/suite"
	"golang.org/x/crypto/bcrypt"

	"helm.sh/helm/v3/internal/test"
	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/chart/loader"
)

type RegistryGetterSuite struct {
	suite.Suite
	Out                io.Writer
	DockerRegistryHost string
	CacheRootDir       string
	RegistryClient     *Client
	SampleCharts       struct {
		OldTag    *chart.Chart
		NewTag    *chart.Chart
		LatestTag *chart.Chart
	}
}

func (suite *RegistryGetterSuite) SetupTest() {
	suite.CacheRootDir = testCacheRootDir
	os.RemoveAll(suite.CacheRootDir)
	os.Mkdir(suite.CacheRootDir, 0700)

	var out bytes.Buffer
	suite.Out = &out
	credentialsFile := filepath.Join(suite.CacheRootDir, CredentialsFileBasename)

	client, err := auth.NewClient(credentialsFile)
	suite.Nil(err, "no error creating auth client")

	resolver, err := client.Resolver(context.Background(), http.DefaultClient, false)
	suite.Nil(err, "no error creating resolver")

	// create cache
	cache, err := NewCache(
		CacheOptDebug(true),
		CacheOptWriter(suite.Out),
		CacheOptRoot(filepath.Join(suite.CacheRootDir, CacheRootDir)),
	)
	suite.Nil(err, "no error creating cache")

	// init test client
	suite.RegistryClient, err = NewClient(
		ClientOptDebug(true),
		ClientOptWriter(suite.Out),
		ClientOptAuthorizer(&Authorizer{
			Client: client,
		}),
		ClientOptResolver(&Resolver{
			Resolver: resolver,
		}),
		ClientOptCache(cache),
	)
	suite.Nil(err, "no error creating registry client")

	// create htpasswd file (w BCrypt, which is required)
	pwBytes, err := bcrypt.GenerateFromPassword([]byte(testPassword), bcrypt.DefaultCost)
	suite.Nil(err, "no error generating bcrypt password for test htpasswd file")
	htpasswdPath := filepath.Join(suite.CacheRootDir, testHtpasswdFileBasename)
	err = ioutil.WriteFile(htpasswdPath, []byte(fmt.Sprintf("%s:%s\n", testUsername, string(pwBytes))), 0644)
	suite.Nil(err, "no error creating test htpasswd file")

	// Registry config
	config := &configuration.Configuration{}
	port, err := test.GetFreePort()
	suite.Nil(err, "no error finding free port for test registry")
	suite.DockerRegistryHost = fmt.Sprintf("localhost:%d", port)
	config.HTTP.Addr = fmt.Sprintf(":%d", port)
	config.HTTP.DrainTimeout = time.Duration(10) * time.Second
	config.Storage = map[string]configuration.Parameters{"inmemory": map[string]interface{}{}}
	config.Auth = configuration.Auth{
		"htpasswd": configuration.Parameters{
			"realm": "localhost",
			"path":  htpasswdPath,
		},
	}
	dockerRegistry, err := registry.NewRegistry(context.Background(), config)
	suite.Nil(err, "no error creating test registry")

	// Start Docker registry
	go dockerRegistry.ListenAndServe()
	suite.RegistryClient.Login(suite.DockerRegistryHost, testUsername, testPassword, false)

	ref1, _ := ParseReference(fmt.Sprintf("%s/testrepo/testchart:0.1.0", suite.DockerRegistryHost))
	ref2, _ := ParseReference(fmt.Sprintf("%s/testrepo/testchart:1.2.3", suite.DockerRegistryHost))
	ref2Latest, _ := ParseReference(fmt.Sprintf("%s/testrepo/testchart:latest", suite.DockerRegistryHost))

	ch1 := &chart.Chart{}
	ch1.Metadata = &chart.Metadata{
		APIVersion: "v1",
		Name:       "testchart",
		Version:    "0.1.0",
	}
	err = suite.RegistryClient.SaveChart(ch1, ref1)
	suite.Nil(err)
	err = suite.RegistryClient.PushChart(ref1)
	suite.Nil(err)

	ch2 := &chart.Chart{}
	ch2.Metadata = &chart.Metadata{
		APIVersion: "v1",
		Name:       "testchart",
		Version:    "1.2.3",
	}

	err = suite.RegistryClient.SaveChart(ch2, ref2)
	suite.Nil(err)
	err = suite.RegistryClient.PushChart(ref2)
	suite.Nil(err)
	err = suite.RegistryClient.SaveChart(ch2, ref2Latest)
	suite.Nil(err)
	err = suite.RegistryClient.PushChart(ref2Latest)
	suite.Nil(err)

	suite.SampleCharts = struct {
		OldTag    *chart.Chart
		NewTag    *chart.Chart
		LatestTag *chart.Chart
	}{OldTag: ch1, NewTag: ch2, LatestTag: ch2}
}

func (suite *RegistryGetterSuite) TearDownSuite() {
	os.RemoveAll(suite.CacheRootDir)
	suite.RegistryClient.Logout(suite.DockerRegistryHost)
}

func (suite *RegistryGetterSuite) TestValidRegistryUrlWithImageTag() {
	g := NewRegistryGetter(suite.RegistryClient)
	res, err := g.Get(fmt.Sprintf("oci://%s/testrepo/testchart:1.2.3", suite.DockerRegistryHost))
	suite.Nil(err, "failed to retrieve chart")

	ch, err := loader.LoadArchive(res)
	suite.Nil(err, "failed to load archive")
	suite.Equal("testchart", ch.Name())
	suite.Equal("1.2.3", ch.Metadata.Version)
}

func (suite *RegistryGetterSuite) TestAppendsVersionToURL() {
	g := NewRegistryGetter(suite.RegistryClient)
	u, err := url.ParseRequestURI(fmt.Sprintf("oci://%s/testrepo/testchart", suite.DockerRegistryHost))
	suite.Nil(err, "failed to parse URI")
	r, err := g.GetWithDetails(u, "0.1.0")
	suite.Nil(err, "failed to retrieve chart")

	ch, err := loader.LoadArchive(r.ChartContent)
	suite.Nil(err, "failed to load chart")
	suite.Equal(suite.SampleCharts.OldTag.Metadata.Version, ch.Metadata.Version)
}

func (suite *RegistryGetterSuite) TestDoesntOverrideTagOnURL() {
	g := NewRegistryGetter(suite.RegistryClient)
	u, err := url.ParseRequestURI(fmt.Sprintf("oci://%s/testrepo/testchart:latest", suite.DockerRegistryHost))
	suite.Nil(err, "failed to parse OCI URL")
	r, err := g.GetWithDetails(u, "0.1.0")
	suite.Nil(err, "failed to retrieve chart")

	ch, err := loader.LoadArchive(r.ChartContent)
	suite.Nil(err, "failed to load chart")
	suite.Equal(suite.SampleCharts.LatestTag.Metadata.Version, ch.Metadata.Version)
}

func (suite *RegistryGetterSuite) TestErrorsIfNeitherVersionNorURLIsProvided() {
	g := NewRegistryGetter(suite.RegistryClient)
	u, err := url.ParseRequestURI(fmt.Sprintf("oci://%s/testrepo/testchart", suite.DockerRegistryHost))
	suite.Nil(err, "failed to parse OCI URL")
	_, err = g.GetWithDetails(u, "")
	suite.NotNil(err, "URL conversion succeeded")
}

func TestRegistryGetterSuite(t *testing.T) {
	suite.Run(t, &RegistryGetterSuite{})
}
