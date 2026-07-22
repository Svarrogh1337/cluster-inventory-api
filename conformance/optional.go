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
	"encoding/json"
	"fmt"
	"slices"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"
	clientcmdv1 "k8s.io/client-go/tools/clientcmd/api/v1"

	cpv1alpha1 "sigs.k8s.io/cluster-inventory-api/apis/v1alpha1"
)

// kep5339 is the access provider plugin specification referenced by the
// extended conformance specs.
const kep5339 = "https://github.com/kubernetes/enhancements/tree/master/keps/sig-multicluster/5339-clusterprofile-plugin-credentials"

// clusterExecExtensionKey is the cluster extension reserved by KEP-5339 for
// the exec plugin configuration.
//
// KEP-5339 also reserves the .../exec/additional-args and .../exec/additional-envs
// extensions with bare YAML string-array payloads, but the published CRD schema
// requires extension payloads to be objects, so those cannot currently be
// persisted and are not covered here.
const clusterExecExtensionKey = "client.authentication.k8s.io/exec"

// listClusterManagerManagedProfiles returns all ClusterProfile objects in the
// cluster except the ones created by this suite.
func listClusterManagerManagedProfiles(ctx context.Context) []cpv1alpha1.ClusterProfile {
	list, err := clusterProfileClient.ApisV1alpha1().ClusterProfiles(metav1.NamespaceAll).List(ctx, metav1.ListOptions{})
	gomega.Expect(err).ToNot(gomega.HaveOccurred())

	return slices.DeleteFunc(list.Items, func(profile cpv1alpha1.ClusterProfile) bool {
		return profile.Spec.ClusterManager.Name == clusterManagerName
	})
}

// awaitWatchEvent reads from the watcher until an event of the given type for
// the named ClusterProfile arrives, the channel closes or the timeout elapses.
func awaitWatchEvent(watcher watch.Interface, eventType watch.EventType, name string) bool {
	timeout := time.After(30 * time.Second)

	for {
		select {
		case event, ok := <-watcher.ResultChan():
			if !ok {
				return false
			}

			profile, isProfile := event.Object.(*cpv1alpha1.ClusterProfile)
			if isProfile && event.Type == eventType && profile.Name == name {
				return true
			}
		case <-timeout:
			return false
		}
	}
}

var _ = ginkgo.Describe("ClusterProfile label conventions", func() {
	SpecifyWithSpecRef("A cluster manager should label the ClusterProfile objects it creates with the cluster manager label",
		kep4322Ref("cluster-manager"),
		ginkgo.Label(OptionalLabel), func(ctx context.Context) {
			profiles := listClusterManagerManagedProfiles(ctx)
			if len(profiles) == 0 {
				ginkgo.Skip("no cluster-manager-managed ClusterProfile objects exist to verify")
			}

			for i := range profiles {
				profile := &profiles[i]
				gomega.Expect(profile.Labels[cpv1alpha1.LabelClusterManagerKey]).To(gomega.Equal(profile.Spec.ClusterManager.Name),
					reportNonConformant(fmt.Sprintf(
						"ClusterProfile %s/%s should carry the %q label with the value of spec.clusterManager.name",
						profile.Namespace, profile.Name, cpv1alpha1.LabelClusterManagerKey)))
			}
		})

	SpecifyWithSpecRef("A namespace representing a clusterset should carry the clusterset label with the clusterset name as value",
		kep4322Ref("whats-the-relationship-between-a-cluster-inventory-and-clusterset"),
		ginkgo.Label(OptionalLabel), func(ctx context.Context) {
			namespaceNames := map[string]bool{}
			for _, profile := range listClusterManagerManagedProfiles(ctx) {
				namespaceNames[profile.Namespace] = true
			}

			labeled := 0

			for namespaceName := range namespaceNames {
				profileNamespace, err := kubernetesClient.CoreV1().Namespaces().Get(ctx, namespaceName, metav1.GetOptions{})
				gomega.Expect(err).ToNot(gomega.HaveOccurred())

				value, ok := profileNamespace.Labels[cpv1alpha1.LabelClusterSetKey]
				if !ok {
					continue
				}

				labeled++
				gomega.Expect(value).ToNot(gomega.BeEmpty(), reportNonConformant(fmt.Sprintf(
					"the %q label on namespace %q must have the name of the clusterset as value",
					cpv1alpha1.LabelClusterSetKey, namespaceName)))
			}

			if labeled == 0 {
				ginkgo.Skip("no namespaces containing ClusterProfile objects carry the clusterset label")
			}
		})
})

var _ = ginkgo.Describe("ClusterProfile access providers", func() {
	SpecifyWithSpecRef("The cluster must preserve the KEP-5339 exec cluster extension of an access provider",
		kep5339,
		ginkgo.Label(OptionalLabel), func(ctx context.Context) {
			profile := createClusterProfile(ctx, newClusterProfile())

			execRaw, err := json.Marshal(&clientcmdv1.ExecConfig{
				APIVersion:         "client.authentication.k8s.io/v1",
				Command:            "cluster-access-plugin",
				Args:               []string{"get-credentials"},
				InteractiveMode:    clientcmdv1.NeverExecInteractiveMode,
				ProvideClusterInfo: true,
			})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			updated := profile.DeepCopy()
			updated.Status.AccessProviders = []cpv1alpha1.AccessProvider{{
				Name: "conformance-exec",
				Cluster: clientcmdv1.Cluster{
					Server: "https://cluster.example.com:6443",
					Extensions: []clientcmdv1.NamedExtension{
						{Name: clusterExecExtensionKey, Extension: runtime.RawExtension{Raw: execRaw}},
					},
				},
			}}

			_, err = clusterProfileClient.ApisV1alpha1().ClusterProfiles(namespace).UpdateStatus(ctx, updated, metav1.UpdateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred(), reportNonConformant(fmt.Sprintf(
				"an access provider carrying the KEP-5339 exec cluster extension was not accepted: %v", err)))

			retrieved, err := clusterProfileClient.ApisV1alpha1().ClusterProfiles(namespace).Get(ctx, profile.Name, metav1.GetOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gomega.Expect(retrieved.Status.AccessProviders).To(gomega.HaveLen(1), reportNonConformant(
				"an access provider carrying the KEP-5339 exec cluster extension was not persisted"))

			persisted := map[string][]byte{}
			for _, extension := range retrieved.Status.AccessProviders[0].Cluster.Extensions {
				persisted[extension.Name] = extension.Extension.Raw
			}

			gomega.Expect(persisted).To(gomega.HaveKey(clusterExecExtensionKey), reportNonConformant(fmt.Sprintf(
				"the %q cluster extension must be preserved on the access provider", clusterExecExtensionKey)))
			gomega.Expect(string(persisted[clusterExecExtensionKey])).To(gomega.MatchJSON(string(execRaw)),
				reportNonConformant(fmt.Sprintf(
					"the %q cluster extension must be preserved unmodified", clusterExecExtensionKey)))
		})

	SpecifyWithSpecRef("The cluster must store the deprecated credentialProviders field alongside accessProviders "+
		"without merging or dropping entries",
		kep4322Ref("access-providers"),
		ginkgo.Label(OptionalLabel), func(ctx context.Context) {
			profile := createClusterProfile(ctx, newClusterProfile())

			updated := profile.DeepCopy()
			updated.Status.AccessProviders = []cpv1alpha1.AccessProvider{{
				Name:    "conformance-provider",
				Cluster: clientcmdv1.Cluster{Server: "https://access.example.com:6443"},
			}}
			updated.Status.CredentialProviders = []cpv1alpha1.CredentialProvider{
				{
					Name:    "conformance-provider",
					Cluster: clientcmdv1.Cluster{Server: "https://credential.example.com:6443"},
				},
				{
					Name:    "conformance-legacy",
					Cluster: clientcmdv1.Cluster{Server: "https://legacy.example.com:6443"},
				},
			}

			_, err := clusterProfileClient.ApisV1alpha1().ClusterProfiles(namespace).UpdateStatus(ctx, updated, metav1.UpdateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred(), reportNonConformant(fmt.Sprintf(
				"the deprecated status.credentialProviders field must still be accepted alongside status.accessProviders: %v", err)))

			retrieved, err := clusterProfileClient.ApisV1alpha1().ClusterProfiles(namespace).Get(ctx, profile.Name, metav1.GetOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			gomega.Expect(retrieved.Status.AccessProviders).To(gomega.HaveLen(1), reportNonConformant(
				"status.accessProviders must be stored as provided when credentialProviders is also set"))
			gomega.Expect(retrieved.Status.AccessProviders[0].Cluster.Server).To(gomega.Equal("https://access.example.com:6443"))

			gomega.Expect(retrieved.Status.CredentialProviders).To(gomega.HaveLen(2), reportNonConformant(
				"status.credentialProviders must be stored as provided, without merging entries that share a name with accessProviders"))

			for _, provider := range retrieved.Status.CredentialProviders {
				switch provider.Name {
				case "conformance-provider":
					gomega.Expect(provider.Cluster.Server).To(gomega.Equal("https://credential.example.com:6443"), reportNonConformant(
						"a credentialProviders entry sharing a name with an accessProviders entry must be stored unmodified"))
				case "conformance-legacy":
					gomega.Expect(provider.Cluster.Server).To(gomega.Equal("https://legacy.example.com:6443"))
				}
			}
		})
})

var _ = ginkgo.Describe("ClusterProfile consumption", func() {
	SpecifyWithSpecRef("The cluster must deliver watch events for ClusterProfile create, update and delete",
		kep4322Ref("how-should-the-api-be-consumed"),
		ginkgo.Label(OptionalLabel), func(ctx context.Context) {
			watcher, err := clusterProfileClient.ApisV1alpha1().ClusterProfiles(namespace).Watch(ctx, metav1.ListOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred(), reportNonConformant(fmt.Sprintf(
				"a watch on ClusterProfile objects could not be established: %v", err)))
			ginkgo.DeferCleanup(watcher.Stop)

			profile := createClusterProfile(ctx, newClusterProfile())
			gomega.Expect(awaitWatchEvent(watcher, watch.Added, profile.Name)).To(gomega.BeTrue(), reportNonConformant(
				"no Added watch event was delivered for a created ClusterProfile"))

			profile.Spec.DisplayName = "conformance-watched"
			profile, err = clusterProfileClient.ApisV1alpha1().ClusterProfiles(namespace).Update(ctx, profile, metav1.UpdateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gomega.Expect(awaitWatchEvent(watcher, watch.Modified, profile.Name)).To(gomega.BeTrue(), reportNonConformant(
				"no Modified watch event was delivered for an updated ClusterProfile"))

			err = clusterProfileClient.ApisV1alpha1().ClusterProfiles(namespace).Delete(ctx, profile.Name, metav1.DeleteOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gomega.Expect(awaitWatchEvent(watcher, watch.Deleted, profile.Name)).To(gomega.BeTrue(), reportNonConformant(
				"no Deleted watch event was delivered for a deleted ClusterProfile"))
		})

	SpecifyWithSpecRef("The cluster must support ClusterProfile patch requests issued with the generated typed clientset",
		kep4322Ref("how-should-the-api-be-consumed"),
		ginkgo.Label(OptionalLabel), func(ctx context.Context) {
			profile := createClusterProfile(ctx, newClusterProfile())

			patched, err := clusterProfileClient.ApisV1alpha1().ClusterProfiles(namespace).Patch(ctx, profile.Name,
				types.MergePatchType, []byte(`{"spec":{"displayName":"conformance-patched"}}`), metav1.PatchOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred(), reportNonConformant(fmt.Sprintf(
				"a ClusterProfile could not be patched with the generated clientset: %v", err)))
			gomega.Expect(patched.Spec.DisplayName).To(gomega.Equal("conformance-patched"), reportNonConformant(
				"a merge patch issued with the generated clientset was not persisted"))
		})
})
