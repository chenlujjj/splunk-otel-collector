// Copyright Splunk, Inc.
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

package kubeutils

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/util/intstr"

	"github.com/signalfx/splunk-otel-collector/tests/testutils"
	"github.com/signalfx/splunk-otel-collector/tests/testutils/kubeutils/manifests"
)

type OTLPSinkDeployment struct {
	tc           *testutils.Testcase
	cluster      *KindCluster
	sb           *sbRest
	otlpEndpoint string
	apiEndpoint  string
}

func NewOTLPSinkDeployment(cluster *KindCluster) *OTLPSinkDeployment {
	dep := &OTLPSinkDeployment{
		tc:      cluster.Testcase,
		cluster: cluster,
	}
	var err error
	dep.sb, err = newSBRest(cluster.Testcase.Logf)
	require.NoError(dep.tc, err)

	_, port, err := net.SplitHostPort(dep.sb.addr)
	require.NoError(dep.tc, err)
	dep.apiEndpoint = fmt.Sprintf("%s:%s", dep.cluster.HostFromContainer(), port)
	dep.otlpEndpoint = dep.cluster.OTLPEndointFromContainer()

	dep.apply()
	cluster.WaitForPods("otlp-sink-.*", "testing", 2*time.Minute)
	return dep
}

func (dep OTLPSinkDeployment) Teardown() {
	dep.sb.shutdown()
	so, se, err := dep.cluster.Delete(dep.manifests())
	require.NoError(dep.tc, err, "stdout: %s, stderr: %s", so.String(), se.String())
}

func (dep OTLPSinkDeployment) apply() {
	so, se, err := dep.cluster.Apply(dep.manifests())
	require.NoError(dep.tc, err, "stdout: %s, stderr: %s", so.String(), se.String())
}

func (dep OTLPSinkDeployment) manifests() string {
	tenSecs := int64(10)
	ns := manifests.Namespace{Name: "testing"}
	dplymnt := manifests.Deployment{
		Name: "otlp-sink", Namespace: "testing", Replicas: 1,
		Labels: map[string]string{
			"app": "otlp-sink", "component": "otlp-sink",
		},
		Containers: []corev1.Container{
			{Name: "otel-collector",
				Image:           testutils.GetCollectorImage(),
				ImagePullPolicy: corev1.PullIfNotPresent,
				Command:         []string{"/otelcol", "--config=/conf/relay.yaml"},
				Env:             []corev1.EnvVar{{Name: "SPLUNK_MEMORY_TOTAL_MIB", Value: "128"}},
				Ports: []corev1.ContainerPort{
					{
						Name:          "http-forwarder",
						ContainerPort: 6060,
						Protocol:      corev1.ProtocolTCP,
					},
					{
						Name:          "otlp",
						ContainerPort: 4317,
						Protocol:      corev1.ProtocolTCP,
					},
					{
						Name:          "otlp-http",
						ContainerPort: 4318,
						Protocol:      corev1.ProtocolTCP,
					}, {
						Name:          "signalfx",
						ContainerPort: 9943,
						Protocol:      corev1.ProtocolTCP,
					},
					{
						Name:          "sapm",
						ContainerPort: 7276,
						Protocol:      corev1.ProtocolTCP,
					}, {
						Name:          "splunk-hec",
						ContainerPort: 8088,
						Protocol:      corev1.ProtocolTCP,
					},
				},
				ReadinessProbe: &corev1.Probe{
					ProbeHandler: corev1.ProbeHandler{
						HTTPGet: &corev1.HTTPGetAction{
							Path: "/", Port: intstr.FromInt32(13133),
						},
					}},
				LivenessProbe: &corev1.Probe{
					ProbeHandler: corev1.ProbeHandler{
						HTTPGet: &corev1.HTTPGetAction{
							Path: "/", Port: intstr.FromInt32(13133),
						},
					},
					TerminationGracePeriodSeconds: &tenSecs,
				},
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("200m"),
						corev1.ResourceMemory: resource.MustParse("128Mi"),
					},
				},
				VolumeMounts: []corev1.VolumeMount{
					{
						MountPath: "/conf",
						Name:      "otlp-sink-configmap",
					},
				},
			},
		},
		Volumes: []corev1.Volume{
			{
				Name: "otlp-sink-configmap",
				VolumeSource: corev1.VolumeSource{
					ConfigMap: &corev1.ConfigMapVolumeSource{
						LocalObjectReference: corev1.LocalObjectReference{Name: "otlp-sink"},
						Items:                []corev1.KeyToPath{{Key: "relay", Path: "relay.yaml"}},
					},
				},
			},
		},
	}

	cm := manifests.ConfigMap{
		Name:      "otlp-sink",
		Namespace: "testing",
		Data: fmt.Sprintf(`relay: |
  extensions:
    health_check:
    http_forwarder:
      egress:
        endpoint: http://%s
  receivers:
    otlp:
      protocols:
        grpc:
          endpoint: 0.0.0.0:4317
        http:
          endpoint: 0.0.0.0:4318
    sapm:
      endpoint: 0.0.0.0:7276
    signalfx:
      access_token_passthrough: true
      endpoint: 0.0.0.0:9943
    splunk_hec:
      endpoint: 0.0.0.0:8088
  exporters:
    otlp:
      endpoint: %s
      tls:
        insecure: true
  service:
    extensions:
      - health_check
      - http_forwarder
    pipelines:
      logs:
        exporters:
        - otlp
        receivers:
        - otlp
        - splunk_hec
        - signalfx
      metrics:
        exporters:
        - otlp
        receivers:
        - otlp
        - signalfx
        - splunk_hec
      traces:
        exporters:
        - otlp
        receivers:
        - otlp
        - sapm`, dep.apiEndpoint, dep.otlpEndpoint),
	}

	svc := manifests.Service{
		Name: "otlp-sink", Namespace: "testing", Type: corev1.ServiceTypeClusterIP,
		Selector: map[string]string{
			"app":       "otlp-sink",
			"component": "otlp-sink",
		},
		Ports: []corev1.ServicePort{
			{
				Name:       "http-forwarder",
				Port:       26060,
				TargetPort: intstr.FromString("http-forwarder"),
				Protocol:   corev1.ProtocolTCP,
			},
			{
				Name:       "otlp",
				Port:       24317,
				TargetPort: intstr.FromString("otlp"),
				Protocol:   corev1.ProtocolTCP,
			},
			{
				Name:       "otlp-http",
				Port:       24318,
				TargetPort: intstr.FromString("otlp-http"),
				Protocol:   corev1.ProtocolTCP,
			},
			{
				Name:       "signalfx",
				Port:       29943,
				TargetPort: intstr.FromString("signalfx"),
				Protocol:   corev1.ProtocolTCP,
			},
			{
				Name:       "splunk-hec",
				Port:       28088,
				TargetPort: intstr.FromString("splunk-hec"),
				Protocol:   corev1.ProtocolTCP,
			},
			{
				Name:       "sapm",
				Port:       27276,
				TargetPort: intstr.FromString("sapm"),
				Protocol:   corev1.ProtocolTCP,
			},
		},
	}

	return manifests.RenderAll(dep.tc, ns, dplymnt, cm, svc)
}

var _ http.Handler = (*sbRest)(nil)

type sbRest struct {
	server *http.Server
	logf   func(format string, args ...any)
	addr   string
}

func newSBRest(logf func(format string, args ...any)) (*sbRest, error) {
	sbr := &sbRest{logf: logf}
	listener, err := net.Listen("tcp", "0.0.0.0:0") // nolint:gosec // not a production server
	if err != nil {
		return nil, err
	}
	sbr.addr = listener.Addr().String()
	sbr.server = &http.Server{Handler: sbr} // nolint:gosec // not a production server
	go sbr.server.Serve(listener)
	return sbr, nil
}

func (sbr *sbRest) shutdown() {
	sbr.server.Shutdown(context.Background())
}

func (sbr *sbRest) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	defer request.Body.Close()
	msg := &strings.Builder{}
	fmt.Fprintf(msg, "url: %s\n", request.URL)
	fmt.Fprintf(msg, "method: %v\n", request.Method)
	for k, v := range request.Header {
		fmt.Fprintf(msg, "header: %v: %v\n", k, v)
	}
	c, err := io.ReadAll(request.Body)
	if err != nil {
		fmt.Printf("err: %v\n", err)
	}
	body := map[string]any{}
	if err = json.Unmarshal(c, &body); err == nil {
		if c, err = json.MarshalIndent(body, "", " "); err != nil {
			fmt.Printf("jsonMarshalIndent: %v\n", err)
		}
	} else {
		fmt.Printf("jsonUnmarshal: %v\n", err)
	}
	fmt.Fprintf(msg, "body: %v\n\n", string(c))
	sbr.logf("received api request: %s", msg.String())
	writer.WriteHeader(200)
}
