/*
Copyright 2024.

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

package createdresources

import (
	"context"
	"testing"

	kubefloworgv1beta1 "github.com/kubeflow/notebooks/workspaces/controller/api/v1beta1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	authorizationv1 "k8s.io/api/authorization/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	kubernetesfake "k8s.io/client-go/kubernetes/fake"
	metadatafake "k8s.io/client-go/metadata/fake"
	clienttesting "k8s.io/client-go/testing"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/kubeflow/notebooks/workspaces/backend/internal/config"
	modelsCommon "github.com/kubeflow/notebooks/workspaces/backend/internal/models/common"
	repoCommon "github.com/kubeflow/notebooks/workspaces/backend/internal/repositories/common"
)

func TestCreatedResourcesRepository(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "CreatedResources Repository Suite")
}

var _ = Describe("CreatedResourcesRepository.GetCreatedResources", func() {
	const (
		namespaceName = "my-namespace"
		workspaceName = "my-workspace"
		workspaceUID  = types.UID("11111111-1111-1111-1111-111111111111")
	)

	var listVerbs = metav1.Verbs{"get", "list", "watch"}

	// newCreatedResource returns the metadata of a resource which was created by the Workspace.
	newCreatedResource := func(apiVersion, kind, name string) *metav1.PartialObjectMetadata {
		return &metav1.PartialObjectMetadata{
			TypeMeta: metav1.TypeMeta{APIVersion: apiVersion, Kind: kind},
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespaceName,
				Labels: map[string]string{
					modelsCommon.LabelCreatedByWorkspace:    workspaceName,
					modelsCommon.LabelCreatedByWorkspaceUID: string(workspaceUID),
				},
			},
		}
	}

	// newRepository returns a repository for a cluster which contains a created resource of
	// each kind that it serves, and in which the backend has the given permissions.
	newRepository := func(rules []authorizationv1.ResourceRule) (*CreatedResourcesRepository, *metadatafake.FakeMetadataClient) {
		scheme := runtime.NewScheme()
		Expect(kubefloworgv1beta1.AddToScheme(scheme)).To(Succeed())
		workspace := &kubefloworgv1beta1.Workspace{
			ObjectMeta: metav1.ObjectMeta{Name: workspaceName, Namespace: namespaceName, UID: workspaceUID},
		}
		cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(workspace).Build()

		clientset := kubernetesfake.NewClientset()
		clientset.Resources = []*metav1.APIResourceList{
			{
				GroupVersion: "v1",
				APIResources: []metav1.APIResource{
					{Name: "configmaps", Kind: "ConfigMap", Namespaced: true, Verbs: listVerbs},
					{Name: "secrets", Kind: "Secret", Namespaced: true, Verbs: listVerbs},
					// resources which must never be listed
					{Name: "events", Kind: "Event", Namespaced: true, Verbs: listVerbs},
					{Name: "nodes", Kind: "Node", Namespaced: false, Verbs: listVerbs},
					{Name: "pods/log", Kind: "Pod", Namespaced: true, Verbs: metav1.Verbs{"get"}},
					{Name: "bindings", Kind: "Binding", Namespaced: true, Verbs: metav1.Verbs{"create"}},
				},
			},
			{
				GroupVersion: "batch/v1",
				APIResources: []metav1.APIResource{
					{Name: "jobs", Kind: "Job", Namespaced: true, Verbs: listVerbs},
				},
			},
			{
				GroupVersion: "metrics.k8s.io/v1beta1",
				APIResources: []metav1.APIResource{
					{Name: "pods", Kind: "PodMetrics", Namespaced: true, Verbs: listVerbs},
				},
			},
		}
		clientset.PrependReactor("create", "selfsubjectrulesreviews", func(clienttesting.Action) (bool, runtime.Object, error) {
			review := &authorizationv1.SelfSubjectRulesReview{
				Status: authorizationv1.SubjectRulesReviewStatus{ResourceRules: rules},
			}
			return true, review, nil
		})

		metadataScheme := metadatafake.NewTestScheme()
		Expect(metav1.AddMetaToScheme(metadataScheme)).To(Succeed())
		metadataClient := metadatafake.NewSimpleMetadataClient(metadataScheme,
			newCreatedResource("v1", "ConfigMap", "my-configmap"),
			newCreatedResource("v1", "Secret", "my-secret"),
			newCreatedResource("v1", "Event", "my-event"),
			newCreatedResource("batch/v1", "Job", "my-job"),
		)

		return NewCreatedResourcesRepository(&config.EnvConfig{}, cl, clientset, metadataClient, nil), metadataClient
	}

	// listedResources returns the kinds of resources which were listed with the metadata client.
	listedResources := func(metadataClient *metadatafake.FakeMetadataClient) []schema.GroupVersionResource {
		var listed []schema.GroupVersionResource
		for _, action := range metadataClient.Actions() {
			Expect(action.GetVerb()).To(Equal("list"))
			Expect(action.GetNamespace()).To(Equal(namespaceName))
			listed = append(listed, action.GetResource())
		}
		return listed
	}

	// names returns the "{KIND}/{NAME}" of the created resources.
	getNames := func(repo *CreatedResourcesRepository) []string {
		createdResources, err := repo.GetCreatedResources(context.Background(), namespaceName, workspaceName)
		Expect(err).NotTo(HaveOccurred())
		names := make([]string, len(createdResources))
		for i, createdResource := range createdResources {
			names[i] = createdResource.Kind + "/" + createdResource.Name
		}
		return names
	}

	It("lists every tracked kind when the backend may list all resources", func() {
		repo, metadataClient := newRepository([]authorizationv1.ResourceRule{
			{Verbs: []string{"*"}, APIGroups: []string{"*"}, Resources: []string{"*"}},
		})

		Expect(getNames(repo)).To(Equal([]string{"ConfigMap/my-configmap", "Secret/my-secret", "Job/my-job"}))
		Expect(listedResources(metadataClient)).To(ConsistOf(
			schema.GroupVersionResource{Version: "v1", Resource: "configmaps"},
			schema.GroupVersionResource{Version: "v1", Resource: "secrets"},
			schema.GroupVersionResource{Group: "batch", Version: "v1", Resource: "jobs"},
		))
	})

	It("only lists the kinds which the backend is allowed to list", func() {
		repo, metadataClient := newRepository([]authorizationv1.ResourceRule{
			{Verbs: []string{"get", "list"}, APIGroups: []string{""}, Resources: []string{"configmaps"}},
			// a rule which does not allow a list of all the resources of a kind
			{Verbs: []string{"get"}, APIGroups: []string{"batch"}, Resources: []string{"jobs"}},
			{Verbs: []string{"list"}, APIGroups: []string{""}, Resources: []string{"secrets"}, ResourceNames: []string{"my-secret"}},
		})

		Expect(getNames(repo)).To(Equal([]string{"ConfigMap/my-configmap"}))
		Expect(listedResources(metadataClient)).To(ConsistOf(
			schema.GroupVersionResource{Version: "v1", Resource: "configmaps"},
		))
	})

	It("returns an empty list when the backend is not allowed to list anything", func() {
		repo, metadataClient := newRepository(nil)

		createdResources, err := repo.GetCreatedResources(context.Background(), namespaceName, workspaceName)
		Expect(err).NotTo(HaveOccurred())
		Expect(createdResources).NotTo(BeNil())
		Expect(createdResources).To(BeEmpty())
		Expect(metadataClient.Actions()).To(BeEmpty())
	})

	It("serves a repeated request from the cache", func() {
		repo, metadataClient := newRepository([]authorizationv1.ResourceRule{
			{Verbs: []string{"list"}, APIGroups: []string{""}, Resources: []string{"configmaps"}},
		})

		Expect(getNames(repo)).To(Equal([]string{"ConfigMap/my-configmap"}))
		Expect(getNames(repo)).To(Equal([]string{"ConfigMap/my-configmap"}))
		Expect(metadataClient.Actions()).To(HaveLen(1))
	})

	It("returns ErrWorkspaceNotFound for a Workspace which does not exist", func() {
		repo, _ := newRepository(nil)

		_, err := repo.GetCreatedResources(context.Background(), namespaceName, "unknown-workspace")
		Expect(err).To(MatchError(repoCommon.ErrWorkspaceNotFound))
	})
})
