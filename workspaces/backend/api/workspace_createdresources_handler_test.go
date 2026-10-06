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

package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"

	"github.com/julienschmidt/httprouter"
	kubefloworgv1beta1 "github.com/kubeflow/notebooks/workspaces/controller/api/v1beta1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/kubeflow/notebooks/workspaces/backend/api/constants"
	commonModels "github.com/kubeflow/notebooks/workspaces/backend/internal/models/common"
	models "github.com/kubeflow/notebooks/workspaces/backend/internal/models/workspaces/createdresources"
)

var _ = Describe("Workspace Created Resources Handler", func() {

	// NOTE: the created resources of a Workspace are cached for a few seconds, so all the
	//       resources are created before the first request, and the tests only read them
	Context("with existing Workspaces", Serial, Ordered, func() {
		const (
			namespaceName = "createdresources-test-ns"
			uniqueName    = "createdresources-test"

			// a user who may get the Workspaces and list ConfigMaps, but nothing else
			configMapUser = "createdresources-configmap-user"
		)
		var (
			workspaceKindName string
			workspace         *kubefloworgv1beta1.Workspace
			otherWorkspace    *kubefloworgv1beta1.Workspace
		)

		// createdByLabels returns the labels which the controller sets on a resource created by a Workspace.
		createdByLabels := func(workspaceName string, workspaceUID types.UID) map[string]string {
			return map[string]string{
				commonModels.LabelCreatedByWorkspace:    workspaceName,
				commonModels.LabelCreatedByWorkspaceUID: string(workspaceUID),
			}
		}

		// getCreatedResources returns the resources created by a Workspace, as seen by a user.
		getCreatedResources := func(user, workspaceName string) []models.CreatedResource {
			rs := doCreatedResourcesRequest(user, namespaceName, workspaceName)
			defer rs.Body.Close()

			body, err := io.ReadAll(rs.Body)
			Expect(err).NotTo(HaveOccurred())
			Expect(rs.StatusCode).To(Equal(http.StatusOK), descUnexpectedHTTPStatus, string(body))

			var response CreatedResourceListEnvelope
			Expect(json.Unmarshal(body, &response)).To(Succeed())
			return response.Data
		}

		BeforeAll(func() {
			workspaceKindName = fmt.Sprintf("workspacekind-%s", uniqueName)

			By("creating the Namespace")
			namespace := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{Name: namespaceName},
			}
			Expect(k8sClient.Create(ctx, namespace)).To(Succeed())

			By("creating the Role and RoleBinding of the ConfigMap user")
			Expect(k8sClient.Create(ctx, &rbacv1.Role{
				ObjectMeta: metav1.ObjectMeta{Name: configMapUser, Namespace: namespaceName},
				Rules: []rbacv1.PolicyRule{
					{APIGroups: []string{kubefloworgv1beta1.GroupVersion.Group}, Resources: []string{"workspaces"}, Verbs: []string{"get"}},
					{APIGroups: []string{""}, Resources: []string{"configmaps"}, Verbs: []string{"list"}},
				},
			})).To(Succeed())
			Expect(k8sClient.Create(ctx, &rbacv1.RoleBinding{
				ObjectMeta: metav1.ObjectMeta{Name: configMapUser, Namespace: namespaceName},
				RoleRef:    rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "Role", Name: configMapUser},
				Subjects:   []rbacv1.Subject{{APIGroup: rbacv1.GroupName, Kind: rbacv1.UserKind, Name: configMapUser}},
			})).To(Succeed())

			By("creating the WorkspaceKind")
			Expect(k8sClient.Create(ctx, NewExampleWorkspaceKind(workspaceKindName))).To(Succeed())

			By("creating the Workspaces")
			workspace = NewExampleWorkspace(fmt.Sprintf("workspace-%s", uniqueName), namespaceName, workspaceKindName)
			Expect(k8sClient.Create(ctx, workspace)).To(Succeed())
			otherWorkspace = NewExampleWorkspace(fmt.Sprintf("workspace-%s-other", uniqueName), namespaceName, workspaceKindName)
			Expect(k8sClient.Create(ctx, otherWorkspace)).To(Succeed())

			By("creating resources which were created by the Workspace")
			Expect(k8sClient.Create(ctx, &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "created-retained",
					Namespace: namespaceName,
					Labels:    createdByLabels(workspace.Name, workspace.UID),
				},
			})).To(Succeed())
			Expect(k8sClient.Create(ctx, &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "created-deleted",
					Namespace: namespaceName,
					Labels:    createdByLabels(workspace.Name, workspace.UID),
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion: kubefloworgv1beta1.GroupVersion.String(),
							Kind:       "Workspace",
							Name:       workspace.Name,
							UID:        workspace.UID,
						},
					},
				},
			})).To(Succeed())
			Expect(k8sClient.Create(ctx, &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "created-secret",
					Namespace: namespaceName,
					Labels:    createdByLabels(workspace.Name, workspace.UID),
				},
			})).To(Succeed())
			Expect(k8sClient.Create(ctx, &rbacv1.Role{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "created-role",
					Namespace: namespaceName,
					Labels:    createdByLabels(workspace.Name, workspace.UID),
				},
			})).To(Succeed())

			By("creating a resource which was created by the other Workspace")
			Expect(k8sClient.Create(ctx, &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "created-by-other",
					Namespace: namespaceName,
					Labels:    createdByLabels(otherWorkspace.Name, otherWorkspace.UID),
				},
			})).To(Succeed())

			By("creating a resource which was retained from a deleted Workspace with the same name")
			Expect(k8sClient.Create(ctx, &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "created-by-predecessor",
					Namespace: namespaceName,
					Labels:    createdByLabels(workspace.Name, "00000000-0000-0000-0000-000000000000"),
				},
			})).To(Succeed())

			By("creating a resource which was not created by a Workspace")
			Expect(k8sClient.Create(ctx, &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "not-created",
					Namespace: namespaceName,
				},
			})).To(Succeed())
		})

		AfterAll(func() {
			By("deleting the Workspaces")
			Expect(k8sClient.Delete(ctx, workspace)).To(Succeed())
			Expect(k8sClient.Delete(ctx, otherWorkspace)).To(Succeed())

			By("deleting the WorkspaceKind")
			workspaceKind := &kubefloworgv1beta1.WorkspaceKind{
				ObjectMeta: metav1.ObjectMeta{Name: workspaceKindName},
			}
			Expect(k8sClient.Delete(ctx, workspaceKind)).To(Succeed())

			By("deleting the Namespace")
			namespace := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{Name: namespaceName},
			}
			Expect(k8sClient.Delete(ctx, namespace)).To(Succeed())
		})

		It("should return the resources created by the Workspace, sorted by group, kind, and name", func() {
			createdResources := getCreatedResources(adminUser, workspace.Name)

			type summary struct {
				group, version, resource, kind, name string
				deletionPolicy                       kubefloworgv1beta1.WorkspaceKindCreatedResourcesDeletionPolicy
			}
			summaries := make([]summary, len(createdResources))
			for i, createdResource := range createdResources {
				summaries[i] = summary{
					group:          createdResource.Group,
					version:        createdResource.Version,
					resource:       createdResource.Resource,
					kind:           createdResource.Kind,
					name:           createdResource.Name,
					deletionPolicy: createdResource.DeletionPolicy,
				}
				Expect(createdResource.Audit.CreatedAt.IsZero()).To(BeFalse())
			}
			Expect(summaries).To(Equal([]summary{
				{"", "v1", "configmaps", "ConfigMap", "created-deleted", kubefloworgv1beta1.WorkspaceKindCreatedResourcesDeletionPolicyDelete},
				{"", "v1", "configmaps", "ConfigMap", "created-retained", kubefloworgv1beta1.WorkspaceKindCreatedResourcesDeletionPolicyRetain},
				{"", "v1", "secrets", "Secret", "created-secret", kubefloworgv1beta1.WorkspaceKindCreatedResourcesDeletionPolicyRetain},
				{"rbac.authorization.k8s.io", "v1", "roles", "Role", "created-role", kubefloworgv1beta1.WorkspaceKindCreatedResourcesDeletionPolicyRetain},
			}))
		})

		It("should not return the resources created by another Workspace", func() {
			createdResources := getCreatedResources(adminUser, otherWorkspace.Name)

			Expect(createdResources).To(HaveLen(1))
			Expect(createdResources[0].Name).To(Equal("created-by-other"))
		})

		It("should only return the kinds of resources which the user is allowed to list", func() {
			// NOTE: the API server may not have seen the RoleBinding of the user for the first request,
			//       and the authorizer of the backend caches that denial for a few seconds
			Eventually(func(g Gomega) {
				rs := doCreatedResourcesRequest(configMapUser, namespaceName, workspace.Name)
				defer rs.Body.Close()
				g.Expect(rs.StatusCode).To(Equal(http.StatusOK))

				var response CreatedResourceListEnvelope
				g.Expect(json.NewDecoder(rs.Body).Decode(&response)).To(Succeed())
				names := make([]string, len(response.Data))
				for i, createdResource := range response.Data {
					names[i] = createdResource.Name
				}
				g.Expect(names).To(Equal([]string{"created-deleted", "created-retained"}))
			}, time.Second*30, time.Second).Should(Succeed())
		})

		It("should return 403 Forbidden when the user is not allowed to get the Workspace", func() {
			rs := doCreatedResourcesRequest("createdresources-unknown-user", namespaceName, workspace.Name)
			defer rs.Body.Close()

			Expect(rs.StatusCode).To(Equal(http.StatusForbidden))
		})
	})

	Context("when querying workspace created resources errors", func() {
		const testNamespace = "ns-createdresources-test"
		const testWorkspace = "my-workspace"

		It("should return 404 when workspace does not exist", func() {
			rs := doCreatedResourcesRequest(adminUser, testNamespace, testWorkspace)
			defer rs.Body.Close()

			Expect(rs.StatusCode).To(Equal(http.StatusNotFound))
		})

		It("should return 422 for invalid parameters", func() {
			rs := doCreatedResourcesRequest(adminUser, "INVALID!!!", testWorkspace)
			defer rs.Body.Close()

			Expect(rs.StatusCode).To(Equal(http.StatusUnprocessableEntity))
		})
	})
})

func doCreatedResourcesRequest(user, namespace, workspace string) *http.Response {
	path := strings.Replace(constants.WorkspaceCreatedResourcesPath, ":"+constants.NamespacePathParam, namespace, 1)
	path = strings.Replace(path, ":"+constants.ResourceNamePathParam, workspace, 1)
	req, err := http.NewRequest(http.MethodGet, path, http.NoBody)
	Expect(err).NotTo(HaveOccurred())
	req.Header.Set(userIdHeader, user)

	ps := httprouter.Params{
		httprouter.Param{Key: constants.NamespacePathParam, Value: namespace},
		httprouter.Param{Key: constants.ResourceNamePathParam, Value: workspace},
	}
	rr := httptest.NewRecorder()
	a.GetWorkspaceCreatedResourcesHandler(rr, req, ps)
	return rr.Result()
}
