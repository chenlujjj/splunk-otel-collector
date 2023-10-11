// Copyright Splunk, Inc.
// Copyright The OpenTelemetry Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//       http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

//go:build testutils

package kubeutils

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/signalfx/splunk-otel-collector/tests/testutils"
)

func TestKindClusterConfig(t *testing.T) {
	tc := testutils.NewTestcase(t)
	cluster := NewKindCluster(tc)
	cluster.ExposedPorts[1] = 2
	cluster.ExposedPorts[3] = 4
	require.Equal(
		tc, `kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
nodes:
  - role: control-plane
    extraPortMappings:
      - containerPort: 2
        hostPort: 1
        listenAddress: "0.0.0.0"
        protocol: tcp
      - containerPort: 4
        hostPort: 3
        listenAddress: "0.0.0.0"
        protocol: tcp
`, cluster.renderConfig())
}
