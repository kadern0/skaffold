/*
Copyright 2021 The Skaffold Authors

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

package runner

import (
	"context"
	"reflect"
	"testing"

	"github.com/GoogleContainerTools/skaffold/pkg/skaffold/access"
	"github.com/GoogleContainerTools/skaffold/pkg/skaffold/config"
	"github.com/GoogleContainerTools/skaffold/pkg/skaffold/debug"
	"github.com/GoogleContainerTools/skaffold/pkg/skaffold/deploy"
	component "github.com/GoogleContainerTools/skaffold/pkg/skaffold/deploy/component/kubernetes"
	"github.com/GoogleContainerTools/skaffold/pkg/skaffold/deploy/helm"
	"github.com/GoogleContainerTools/skaffold/pkg/skaffold/deploy/kpt"
	"github.com/GoogleContainerTools/skaffold/pkg/skaffold/deploy/kubectl"
	"github.com/GoogleContainerTools/skaffold/pkg/skaffold/deploy/kustomize"
	"github.com/GoogleContainerTools/skaffold/pkg/skaffold/deploy/label"
	pkgkubectl "github.com/GoogleContainerTools/skaffold/pkg/skaffold/kubectl"
	"github.com/GoogleContainerTools/skaffold/pkg/skaffold/kubernetes"
	k8sloader "github.com/GoogleContainerTools/skaffold/pkg/skaffold/kubernetes/loader"
	k8slogger "github.com/GoogleContainerTools/skaffold/pkg/skaffold/kubernetes/logger"
	"github.com/GoogleContainerTools/skaffold/pkg/skaffold/kubernetes/portforward"
	k8sstatus "github.com/GoogleContainerTools/skaffold/pkg/skaffold/kubernetes/status"
	"github.com/GoogleContainerTools/skaffold/pkg/skaffold/loader"
	"github.com/GoogleContainerTools/skaffold/pkg/skaffold/runner/runcontext"
	latestV1 "github.com/GoogleContainerTools/skaffold/pkg/skaffold/schema/latest/v1"
	"github.com/GoogleContainerTools/skaffold/pkg/skaffold/status"
	"github.com/GoogleContainerTools/skaffold/pkg/skaffold/sync"
	"github.com/GoogleContainerTools/skaffold/pkg/skaffold/util"
	"github.com/GoogleContainerTools/skaffold/testutil"
)

func TestGetDeployer(tOuter *testing.T) {
	testutil.Run(tOuter, "TestGetDeployer", func(t *testutil.T) {
		tests := []struct {
			description string
			cfg         latestV1.DeployType
			helmVersion string
			expected    deploy.Deployer
			apply       bool
			shouldErr   bool
		}{
			{
				description: "no deployer",
				expected:    deploy.DeployerMux{},
			},
			{
				description: "helm deployer with 3.0.0 version",
				cfg:         latestV1.DeployType{HelmDeploy: &latestV1.HelmDeploy{}},
				helmVersion: `version.BuildInfo{Version:"v3.0.0"}`,
				expected:    deploy.NewDeployerMux([]deploy.Deployer{&helm.Deployer{}}, false),
			},
			{
				description: "helm deployer with less than 3.0.0 version",
				cfg:         latestV1.DeployType{HelmDeploy: &latestV1.HelmDeploy{}},
				helmVersion: "2.0.0",
				shouldErr:   true,
			},
			{
				description: "kubectl deployer",
				cfg:         latestV1.DeployType{KubectlDeploy: &latestV1.KubectlDeploy{}},
				expected: deploy.NewDeployerMux([]deploy.Deployer{
					t.RequireNonNilResult(kubectl.NewDeployer(&runcontext.RunContext{
						Pipelines: runcontext.NewPipelines([]latestV1.Pipeline{{}}),
					}, &label.DefaultLabeller{}, &latestV1.KubectlDeploy{
						Flags: latestV1.KubectlFlags{},
					})).(deploy.Deployer),
				}, false),
			},
			{
				description: "kustomize deployer",
				cfg:         latestV1.DeployType{KustomizeDeploy: &latestV1.KustomizeDeploy{}},
				expected: deploy.NewDeployerMux([]deploy.Deployer{
					t.RequireNonNilResult(kustomize.NewDeployer(&runcontext.RunContext{
						Pipelines: runcontext.NewPipelines([]latestV1.Pipeline{{}}),
					}, &label.DefaultLabeller{}, &latestV1.KustomizeDeploy{
						Flags: latestV1.KubectlFlags{},
					})).(deploy.Deployer),
				}, false),
			},
			{
				description: "kpt deployer",
				cfg:         latestV1.DeployType{KptDeploy: &latestV1.KptDeploy{}},
				expected: deploy.NewDeployerMux([]deploy.Deployer{
					&kpt.Deployer{},
				}, false),
			},
			{
				description: "apply forces creation of kubectl deployer with kpt config",
				cfg:         latestV1.DeployType{KptDeploy: &latestV1.KptDeploy{}},
				apply:       true,
				expected: t.RequireNonNilResult(kubectl.NewDeployer(&runcontext.RunContext{
					Pipelines: runcontext.NewPipelines([]latestV1.Pipeline{{}}),
				}, &label.DefaultLabeller{}, &latestV1.KubectlDeploy{
					Flags: latestV1.KubectlFlags{},
				})).(deploy.Deployer),
			},
			{
				description: "apply forces creation of kubectl deployer with helm config",
				cfg:         latestV1.DeployType{HelmDeploy: &latestV1.HelmDeploy{}},
				helmVersion: `version.BuildInfo{Version:"v3.0.0"}`,
				apply:       true,
				expected: t.RequireNonNilResult(kubectl.NewDeployer(&runcontext.RunContext{
					Pipelines: runcontext.NewPipelines([]latestV1.Pipeline{{}}),
				}, &label.DefaultLabeller{}, &latestV1.KubectlDeploy{
					Flags: latestV1.KubectlFlags{},
				})).(deploy.Deployer),
			},
			{
				description: "multiple deployers",
				cfg: latestV1.DeployType{
					HelmDeploy: &latestV1.HelmDeploy{},
					KptDeploy:  &latestV1.KptDeploy{},
				},
				helmVersion: `version.BuildInfo{Version:"v3.0.0"}`,
				expected: deploy.NewDeployerMux([]deploy.Deployer{
					&helm.Deployer{},
					&kpt.Deployer{},
				}, false),
			},
		}
		for _, test := range tests {
			testutil.Run(tOuter, test.description, func(t *testutil.T) {
				if test.helmVersion != "" {
					t.Override(&util.DefaultExecCommand, testutil.CmdRunWithOutput(
						"helm version --client",
						test.helmVersion,
					))
				}

				deployer, err := GetDeployer(context.Background(), &runcontext.RunContext{
					Opts: config.SkaffoldOptions{
						Apply: test.apply,
					},
					Pipelines: runcontext.NewPipelines([]latestV1.Pipeline{{
						Deploy: latestV1.DeployConfig{
							DeployType: test.cfg,
						},
					}}),
				}, &label.DefaultLabeller{})

				t.CheckError(test.shouldErr, err)
				t.CheckTypeEquality(test.expected, deployer)

				if reflect.TypeOf(test.expected) == reflect.TypeOf(deploy.DeployerMux{}) {
					expected := test.expected.(deploy.DeployerMux).GetDeployers()
					deployers := deployer.(deploy.DeployerMux).GetDeployers()
					t.CheckDeepEqual(len(expected), len(deployers))
					for i, v := range expected {
						t.CheckTypeEquality(v, deployers[i])
					}
				}
			})
		}
	})
}

func TestGetDefaultDeployer(tOuter *testing.T) {
	testutil.Run(tOuter, "TestGetDeployer", func(t *testutil.T) {
		t.Override(&component.NewAccessor, func(portforward.Config, string, *pkgkubectl.CLI, kubernetes.PodSelector, label.Config, *[]string) access.Accessor {
			return &access.NoopAccessor{}
		})
		t.Override(&component.NewDebugger, func(config.RunMode, kubernetes.PodSelector, *[]string, string) debug.Debugger {
			return &debug.NoopDebugger{}
		})
		t.Override(&component.NewMonitor, func(k8sstatus.Config, string, *label.DefaultLabeller, *[]string) status.Monitor {
			return &status.NoopMonitor{}
		})
		t.Override(&component.NewImageLoader, func(k8sloader.Config, *pkgkubectl.CLI) loader.ImageLoader {
			return &loader.NoopImageLoader{}
		})
		t.Override(&component.NewSyncer, func(*pkgkubectl.CLI, *[]string, k8slogger.Formatter) sync.Syncer {
			return &sync.NoopSyncer{}
		})
		t.Override(&component.NewLogger, func(k8slogger.Config, *pkgkubectl.CLI, kubernetes.PodSelector, *[]string) k8slogger.Logger {
			return &k8slogger.NoopLogger{}
		})
		tests := []struct {
			name      string
			cfgs      []latestV1.DeployType
			expected  *kubectl.Deployer
			shouldErr bool
		}{
			{
				name: "one config with kubectl deploy",
				cfgs: []latestV1.DeployType{{
					KubectlDeploy: &latestV1.KubectlDeploy{},
				}},
				expected: t.RequireNonNilResult(kubectl.NewDeployer(&runcontext.RunContext{
					Pipelines: runcontext.NewPipelines([]latestV1.Pipeline{{}}),
				}, &label.DefaultLabeller{}, &latestV1.KubectlDeploy{
					Flags: latestV1.KubectlFlags{},
				})).(*kubectl.Deployer),
			},
			{
				name: "one config with kubectl deploy, with flags",
				cfgs: []latestV1.DeployType{{
					KubectlDeploy: &latestV1.KubectlDeploy{
						Flags: latestV1.KubectlFlags{
							Apply:  []string{"--foo"},
							Global: []string{"--bar"},
						},
					},
				}},
				expected: t.RequireNonNilResult(kubectl.NewDeployer(&runcontext.RunContext{
					Pipelines: runcontext.NewPipelines([]latestV1.Pipeline{{}}),
				}, &label.DefaultLabeller{}, &latestV1.KubectlDeploy{
					Flags: latestV1.KubectlFlags{
						Apply:  []string{"--foo"},
						Global: []string{"--bar"},
					},
				})).(*kubectl.Deployer),
			},
			{
				name: "two kubectl configs with mismatched flags should fail",
				cfgs: []latestV1.DeployType{
					{
						KubectlDeploy: &latestV1.KubectlDeploy{
							Flags: latestV1.KubectlFlags{
								Apply: []string{"--foo"},
							},
						},
					},
					{
						KubectlDeploy: &latestV1.KubectlDeploy{
							Flags: latestV1.KubectlFlags{
								Apply: []string{"--bar"},
							},
						},
					},
				},
				shouldErr: true,
			},
			{
				name: "one config with helm deploy",
				cfgs: []latestV1.DeployType{{
					HelmDeploy: &latestV1.HelmDeploy{},
				}},
				expected: t.RequireNonNilResult(kubectl.NewDeployer(&runcontext.RunContext{
					Pipelines: runcontext.NewPipelines([]latestV1.Pipeline{{}}),
				}, &label.DefaultLabeller{}, &latestV1.KubectlDeploy{
					Flags: latestV1.KubectlFlags{},
				})).(*kubectl.Deployer),
			},
			{
				name: "one config with kustomize deploy",
				cfgs: []latestV1.DeployType{{
					KustomizeDeploy: &latestV1.KustomizeDeploy{},
				}},
				expected: t.RequireNonNilResult(kubectl.NewDeployer(&runcontext.RunContext{
					Pipelines: runcontext.NewPipelines([]latestV1.Pipeline{{}}),
				}, &label.DefaultLabeller{}, &latestV1.KubectlDeploy{
					Flags: latestV1.KubectlFlags{},
				})).(*kubectl.Deployer),
			},
		}

		for _, test := range tests {
			testutil.Run(tOuter, test.name, func(t *testutil.T) {
				pipelines := []latestV1.Pipeline{}
				for _, cfg := range test.cfgs {
					pipelines = append(pipelines, latestV1.Pipeline{
						Deploy: latestV1.DeployConfig{
							DeployType: cfg,
						},
					})
				}
				deployer, err := getDefaultDeployer(&runcontext.RunContext{
					Pipelines: runcontext.NewPipelines(pipelines),
				}, &label.DefaultLabeller{})

				t.CheckErrorAndFailNow(test.shouldErr, err)

				// if we were expecting an error, this implies that the returned deployer is nil
				// this error was checked in the previous call, so if we didn't fail there (i.e. the encountered error was correct),
				// then the test is finished and we can continue.
				if !test.shouldErr {
					t.CheckTypeEquality(&kubectl.Deployer{}, deployer)

					kDeployer := deployer.(*kubectl.Deployer)
					if !reflect.DeepEqual(kDeployer, test.expected) {
						t.Fail()
					}
				}
			})
		}
	})
}
