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
	"fmt"
	"slices"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/rand"
	clientcmdv1 "k8s.io/client-go/tools/clientcmd/api/v1"

	cpv1alpha1 "sigs.k8s.io/cluster-inventory-api/apis/v1alpha1"
)

// clusterManagerName is the cluster manager the suite's ClusterProfile objects
// claim to be owned by.
const clusterManagerName = "conformance-cluster-manager"

func newClusterProfile() *cpv1alpha1.ClusterProfile {
	name := "conformance-" + rand.String(5)

	return &cpv1alpha1.ClusterProfile{
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: map[string]string{cpv1alpha1.LabelClusterManagerKey: clusterManagerName},
		},
		Spec: cpv1alpha1.ClusterProfileSpec{
			DisplayName: name,
			ClusterManager: cpv1alpha1.ClusterManager{
				Name: clusterManagerName,
			},
		},
	}
}

// createClusterProfile creates the given ClusterProfile, reporting
// non-conformance if a valid object is not accepted, and registers a cleanup
// that removes it when the spec completes.
func createClusterProfile(ctx context.Context, profile *cpv1alpha1.ClusterProfile) *cpv1alpha1.ClusterProfile {
	created, err := clusterProfileClient.ApisV1alpha1().ClusterProfiles(namespace).Create(ctx, profile, metav1.CreateOptions{})
	gomega.Expect(err).ToNot(gomega.HaveOccurred(), reportNonConformant(fmt.Sprintf(
		"a valid ClusterProfile was not accepted: %v", err)))

	ginkgo.DeferCleanup(func(ctx context.Context) {
		err := clusterProfileClient.ApisV1alpha1().ClusterProfiles(namespace).Delete(ctx, created.Name, metav1.DeleteOptions{})
		if !apierrors.IsNotFound(err) {
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
		}
	})

	return created
}

var _ = ginkgo.Describe("ClusterProfile", func() {
	SpecifyWithSpecRef("The cluster must support create, get, list, update and delete of ClusterProfile objects",
		kep4322Ref("design-details"),
		ginkgo.Label(RequiredLabel), func(ctx context.Context) {
			profile := createClusterProfile(ctx, newClusterProfile())

			retrieved, err := clusterProfileClient.ApisV1alpha1().ClusterProfiles(namespace).Get(ctx, profile.Name, metav1.GetOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred(), reportNonConformant(fmt.Sprintf(
				"a created ClusterProfile could not be retrieved: %v", err)))
			gomega.Expect(retrieved.Spec).To(gomega.Equal(profile.Spec), reportNonConformant(
				"the retrieved ClusterProfile spec does not match what was created"))

			list, err := clusterProfileClient.ApisV1alpha1().ClusterProfiles(namespace).List(ctx, metav1.ListOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred(), reportNonConformant(fmt.Sprintf(
				"ClusterProfile objects could not be listed: %v", err)))
			gomega.Expect(slices.ContainsFunc(list.Items, func(item cpv1alpha1.ClusterProfile) bool {
				return item.Name == profile.Name
			})).To(gomega.BeTrue(), reportNonConformant(
				"a created ClusterProfile was not returned when listing"))

			retrieved.Spec.DisplayName = "conformance-updated"
			updated, err := clusterProfileClient.ApisV1alpha1().ClusterProfiles(namespace).Update(ctx, retrieved, metav1.UpdateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred(), reportNonConformant(fmt.Sprintf(
				"the mutable spec.displayName field could not be updated: %v", err)))
			gomega.Expect(updated.Spec.DisplayName).To(gomega.Equal("conformance-updated"), reportNonConformant(
				"an update to spec.displayName was not persisted"))

			err = clusterProfileClient.ApisV1alpha1().ClusterProfiles(namespace).Delete(ctx, profile.Name, metav1.DeleteOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred(), reportNonConformant(fmt.Sprintf(
				"a ClusterProfile could not be deleted: %v", err)))

			gomega.Eventually(func(ctx context.Context) bool {
				_, err := clusterProfileClient.ApisV1alpha1().ClusterProfiles(namespace).Get(ctx, profile.Name, metav1.GetOptions{})
				return apierrors.IsNotFound(err)
			}).WithContext(ctx).Within(30*time.Second).ProbeEvery(250*time.Millisecond).Should(gomega.BeTrue(),
				reportNonConformant("a deleted ClusterProfile was still retrievable"))
		})

	SpecifyWithSpecRef("The cluster must reject a ClusterProfile that does not specify spec.clusterManager.name",
		kep4322Ref("cluster-manager"),
		ginkgo.Label(RequiredLabel), func(ctx context.Context) {
			testCases := []struct {
				desc string
				spec map[string]interface{}
			}{
				{desc: "without spec.clusterManager", spec: map[string]interface{}{"displayName": "conformance"}},
				{desc: "without spec.clusterManager.name", spec: map[string]interface{}{
					"displayName": "conformance", "clusterManager": map[string]interface{}{},
				}},
			}

			gvr := cpv1alpha1.ClusterProfileSchemeGroupVersionResource

			for _, testCase := range testCases {
				obj := &unstructured.Unstructured{Object: map[string]interface{}{
					"apiVersion": cpv1alpha1.GroupVersion.String(),
					"kind":       cpv1alpha1.ClusterProfileKind,
					"metadata":   map[string]interface{}{"generateName": "conformance-invalid-"},
					"spec":       testCase.spec,
				}}

				created, err := dynamicClient.Resource(gvr).Namespace(namespace).Create(ctx, obj, metav1.CreateOptions{})
				if err == nil {
					_ = dynamicClient.Resource(gvr).Namespace(namespace).Delete(ctx, created.GetName(), metav1.DeleteOptions{})
				}

				gomega.Expect(apierrors.IsInvalid(err)).To(gomega.BeTrue(), reportNonConformant(fmt.Sprintf(
					"a ClusterProfile %s must be rejected as invalid; got error: %v", testCase.desc, err)))
			}
		})

	SpecifyWithSpecRef("The cluster must reject updates to the immutable spec.clusterManager.name field",
		kep4322Ref("cluster-manager"),
		ginkgo.Label(RequiredLabel), func(ctx context.Context) {
			profile := createClusterProfile(ctx, newClusterProfile())

			profile.Spec.ClusterManager.Name = clusterManagerName + "-changed"
			_, err := clusterProfileClient.ApisV1alpha1().ClusterProfiles(namespace).Update(ctx, profile, metav1.UpdateOptions{})
			gomega.Expect(apierrors.IsInvalid(err)).To(gomega.BeTrue(), reportNonConformant(fmt.Sprintf(
				"an update changing spec.clusterManager.name must be rejected as invalid; got error: %v", err)))

			unchanged, err := clusterProfileClient.ApisV1alpha1().ClusterProfiles(namespace).Get(ctx, profile.Name, metav1.GetOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gomega.Expect(unchanged.Spec.ClusterManager.Name).To(gomega.Equal(clusterManagerName), reportNonConformant(
				"spec.clusterManager.name must remain unchanged after a rejected update"))
		})

	SpecifyWithSpecRef("The cluster must support updating conditions, version, properties and accessProviders "+
		"via the status subresource without modifying the spec",
		kep4322Ref("status"),
		ginkgo.Label(RequiredLabel), func(ctx context.Context) {
			profile := createClusterProfile(ctx, newClusterProfile())

			updated := profile.DeepCopy()
			updated.Spec.DisplayName = "status-must-not-change-spec"
			updated.Status.Version.Kubernetes = "v1.35.0"
			updated.Status.Properties = []cpv1alpha1.Property{{
				Name:             "clusterset.k8s.io",
				Value:            "conformance",
				LastObservedTime: metav1.Now(),
			}}
			updated.Status.AccessProviders = []cpv1alpha1.AccessProvider{{
				Name:    "conformance-kubeconfig",
				Cluster: clientcmdv1.Cluster{Server: "https://cluster.example.com:6443"},
			}}
			meta.SetStatusCondition(&updated.Status.Conditions, metav1.Condition{
				Type:    cpv1alpha1.ClusterConditionControlPlaneHealthy,
				Status:  metav1.ConditionTrue,
				Reason:  "AsExpected",
				Message: "control plane is healthy",
			})

			afterStatus, err := clusterProfileClient.ApisV1alpha1().ClusterProfiles(namespace).UpdateStatus(ctx, updated, metav1.UpdateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred(), reportNonConformant(fmt.Sprintf(
				"the ClusterProfile status could not be updated via the status subresource: %v", err)))
			gomega.Expect(afterStatus.Spec.DisplayName).To(gomega.Equal(profile.Spec.DisplayName), reportNonConformant(
				"an update via the status subresource must not modify the spec"))

			retrieved, err := clusterProfileClient.ApisV1alpha1().ClusterProfiles(namespace).Get(ctx, profile.Name, metav1.GetOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gomega.Expect(retrieved.Status.Version.Kubernetes).To(gomega.Equal("v1.35.0"), reportNonConformant(
				"status.version.kubernetes was not persisted"))

			gomega.Expect(retrieved.Status.Properties).To(gomega.HaveLen(1), reportNonConformant(
				"status.properties was not persisted"))
			gomega.Expect(retrieved.Status.Properties[0].Name).To(gomega.Equal("clusterset.k8s.io"))
			gomega.Expect(retrieved.Status.Properties[0].Value).To(gomega.Equal("conformance"))

			gomega.Expect(retrieved.Status.AccessProviders).To(gomega.HaveLen(1), reportNonConformant(
				"status.accessProviders was not persisted"))
			gomega.Expect(retrieved.Status.AccessProviders[0].Name).To(gomega.Equal("conformance-kubeconfig"))
			gomega.Expect(retrieved.Status.AccessProviders[0].Cluster.Server).To(gomega.Equal("https://cluster.example.com:6443"))

			condition := meta.FindStatusCondition(retrieved.Status.Conditions, cpv1alpha1.ClusterConditionControlPlaneHealthy)
			gomega.Expect(condition).ToNot(gomega.BeNil(), reportNonConformant(fmt.Sprintf(
				"the %s condition was not persisted", cpv1alpha1.ClusterConditionControlPlaneHealthy)))
			gomega.Expect(condition.LastTransitionTime.IsZero()).To(gomega.BeFalse(), reportNonConformant(
				"a persisted condition must have a lastTransitionTime"))

			retrieved.Status.Version.Kubernetes = "v0.0.0-must-be-ignored"
			afterUpdate, err := clusterProfileClient.ApisV1alpha1().ClusterProfiles(namespace).Update(ctx, retrieved, metav1.UpdateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gomega.Expect(afterUpdate.Status.Version.Kubernetes).To(gomega.Equal("v1.35.0"), reportNonConformant(
				"an update to the main resource must not modify the status when the status subresource is enabled"))
		})

	SpecifyWithSpecRef("The cluster must enforce metav1.Condition conventions on status conditions",
		kep4322Ref("conditions"),
		ginkgo.Label(RequiredLabel), func(ctx context.Context) {
			profile := createClusterProfile(ctx, newClusterProfile())

			invalid := profile.DeepCopy()
			invalid.Status.Conditions = []metav1.Condition{{
				Type:               cpv1alpha1.ClusterConditionControlPlaneHealthy,
				Status:             metav1.ConditionStatus("Degraded"),
				Reason:             "AsExpected",
				Message:            "conformance",
				LastTransitionTime: metav1.Now(),
			}}
			_, err := clusterProfileClient.ApisV1alpha1().ClusterProfiles(namespace).UpdateStatus(ctx, invalid, metav1.UpdateOptions{})
			gomega.Expect(apierrors.IsInvalid(err)).To(gomega.BeTrue(), reportNonConformant(fmt.Sprintf(
				"a condition status other than True, False or Unknown must be rejected as invalid; got error: %v", err)))

			invalid = profile.DeepCopy()
			invalid.Status.Conditions = []metav1.Condition{{
				Type:               cpv1alpha1.ClusterConditionControlPlaneHealthy,
				Status:             metav1.ConditionTrue,
				Reason:             "",
				Message:            "conformance",
				LastTransitionTime: metav1.Now(),
			}}
			_, err = clusterProfileClient.ApisV1alpha1().ClusterProfiles(namespace).UpdateStatus(ctx, invalid, metav1.UpdateOptions{})
			gomega.Expect(apierrors.IsInvalid(err)).To(gomega.BeTrue(), reportNonConformant(fmt.Sprintf(
				"a condition without a reason must be rejected as invalid; got error: %v", err)))
		})
})
