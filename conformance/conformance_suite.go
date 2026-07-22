/*
Copyright The Kubernetes Authors.

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

package conformance

import (
	"context"
	"flag"
	"fmt"
	"testing"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	cpclientset "sigs.k8s.io/cluster-inventory-api/client/clientset/versioned"
)

var (
	loadingRules *clientcmd.ClientConfigLoadingRules
	kubeContext  string
	namespace    string

	organization string
	project      string
	version      string
	url          string

	restConfig           *rest.Config
	kubernetesClient     kubernetes.Interface
	clusterProfileClient cpclientset.Interface
	dynamicClient        dynamic.Interface
	createdNamespace     bool
)

func init() {
	loadingRules = clientcmd.NewDefaultClientConfigLoadingRules()
	flag.StringVar(&loadingRules.ExplicitPath, "kubeconfig", "",
		"absolute path to the kubeconfig file of the cluster serving the ClusterProfile API")
	flag.StringVar(&kubeContext, "context", "",
		"kubeconfig context of the cluster serving the ClusterProfile API")
	flag.StringVar(&namespace, "namespace", "",
		"namespace used for ClusterProfile objects created by the suite; if unset, a temporary namespace "+
			"is created and removed when the suite completes")
	flag.StringVar(&organization, "organization", "",
		"name of the organization responsible for the implementation being tested")
	flag.StringVar(&project, "project", "", "name of the implementation project being tested")
	flag.StringVar(&version, "version", "", "version of the implementation being tested")
	flag.StringVar(&url, "url", "", "URL pointing to the implementation project or its documentation")
}

// TestConformance runs the ClusterProfile API conformance suite. It is
// exported so implementations can import this package and run the suite from
// their own test pipelines.
func TestConformance(t *testing.T) {
	gomega.RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "ClusterProfile API Conformance Suite")
}

var _ = ginkgo.BeforeSuite(func(ctx context.Context) {
	gomega.Expect(setupSuite(ctx)).To(gomega.Succeed(), "Test suite set up failed")
})

var _ = ginkgo.AfterSuite(func(ctx context.Context) {
	if createdNamespace {
		gomega.Expect(kubernetesClient.CoreV1().Namespaces().Delete(ctx, namespace, metav1.DeleteOptions{})).
			To(gomega.Succeed(), "Error deleting the temporary test namespace")
	}
})

func setupSuite(ctx context.Context) error {
	overrides := &clientcmd.ConfigOverrides{ClusterDefaults: clientcmd.ClusterDefaults}
	overrides.CurrentContext = kubeContext

	var err error

	restConfig, err = clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, overrides).ClientConfig()
	if err != nil {
		return fmt.Errorf("error building the Kubernetes client configuration: %w", err)
	}

	kubernetesClient, err = kubernetes.NewForConfig(restConfig)
	if err != nil {
		return fmt.Errorf("error creating the Kubernetes client: %w", err)
	}

	clusterProfileClient, err = cpclientset.NewForConfig(restConfig)
	if err != nil {
		return fmt.Errorf("error creating the ClusterProfile client: %w", err)
	}

	dynamicClient, err = dynamic.NewForConfig(restConfig)
	if err != nil {
		return fmt.Errorf("error creating the dynamic client: %w", err)
	}
	if namespace != "" {
		if _, err = kubernetesClient.CoreV1().Namespaces().Get(ctx, namespace, metav1.GetOptions{}); err != nil {
			return fmt.Errorf("error retrieving the target namespace %q: %w", namespace, err)
		}

		return nil
	}

	namespace = "clusterprofile-conformance-" + rand.String(5)

	_, err = kubernetesClient.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: namespace},
	}, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("error creating the temporary test namespace %q: %w", namespace, err)
	}

	createdNamespace = true

	return nil
}
