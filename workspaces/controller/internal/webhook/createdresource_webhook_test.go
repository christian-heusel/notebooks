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

package webhook

import (
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	admissionv1 "k8s.io/api/admission/v1"
	authenticationv1 "k8s.io/api/authentication/v1"
	coordinationv1 "k8s.io/api/coordination/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	kubefloworgv1beta1 "github.com/kubeflow/notebooks/workspaces/controller/api/v1beta1"
)

var _ = Describe("CreatedResource Webhook", Ordered, func() {

	// NOTE: these tests have their own Namespaces, because they grant every ServiceAccount
	//       in them full access, so that a ServiceAccount can create any kind of resource
	const (
		namespaceName      = "createdresource-webhook-test"
		otherNamespaceName = "createdresource-webhook-test-other"
		uniqueName         = "createdresource-webhook-test"
	)

	var (
		retainWorkspace *kubefloworgv1beta1.Workspace
		deleteWorkspace *kubefloworgv1beta1.Workspace

		retainServiceAccount  *corev1.ServiceAccount
		deleteServiceAccount  *corev1.ServiceAccount
		unownedServiceAccount *corev1.ServiceAccount
	)

	// newClientFor returns a client which makes its requests as the given ServiceAccount.
	// NOTE: the API server gives an impersonated ServiceAccount the same username and groups
	//       as its tokens do, and we set the UID, so the webhook sees what a real Workspace sends
	newClientFor := func(serviceAccount *corev1.ServiceAccount, uid types.UID) client.Client {
		impersonatedCfg := rest.CopyConfig(cfg)
		impersonatedCfg.Impersonate = rest.ImpersonationConfig{
			UserName: fmt.Sprintf("system:serviceaccount:%s:%s", serviceAccount.Namespace, serviceAccount.Name),
			UID:      string(uid),
		}
		impersonatedClient, err := client.New(impersonatedCfg, client.Options{Scheme: scheme.Scheme})
		Expect(err).NotTo(HaveOccurred())
		return impersonatedClient
	}

	// newConfigMap returns a ConfigMap with a generated name.
	newConfigMap := func(namespace string) *corev1.ConfigMap {
		return &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				GenerateName: "created-",
				Namespace:    namespace,
			},
		}
	}

	// createWorkspace creates a WorkspaceKind with the given deletion policy, a Workspace of
	// that kind, and the ServiceAccount which the controller would create for the Workspace.
	createWorkspace := func(name string, deletionPolicy *kubefloworgv1beta1.WorkspaceKindCreatedResourcesDeletionPolicy) (*kubefloworgv1beta1.Workspace, *corev1.ServiceAccount) {
		workspaceKind := NewExampleWorkspaceKind(name)
		if deletionPolicy != nil {
			workspaceKind.Spec.PodTemplate.ServiceAccount = &kubefloworgv1beta1.WorkspaceKindServiceAccount{
				CreatedResources: &kubefloworgv1beta1.WorkspaceKindCreatedResources{
					DeletionPolicy: deletionPolicy,
				},
			}
		}
		Expect(k8sClient.Create(ctx, workspaceKind)).To(Succeed())

		// NOTE: the Workspace webhook reads the WorkspaceKind from a cache,
		//       which may not have seen the one which we have just created
		workspace := NewExampleWorkspace(name, namespaceName, name)
		Eventually(func() error {
			return k8sClient.Create(ctx, workspace)
		}, time.Second*5, time.Millisecond*100).Should(Succeed())

		serviceAccount := &corev1.ServiceAccount{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "ws-" + name,
				Namespace: namespaceName,
			},
		}
		Expect(ctrl.SetControllerReference(workspace, serviceAccount, scheme.Scheme)).To(Succeed())
		Expect(k8sClient.Create(ctx, serviceAccount)).To(Succeed())

		return workspace, serviceAccount
	}

	BeforeAll(func() {
		for _, name := range []string{namespaceName, otherNamespaceName} {
			By(fmt.Sprintf("creating the %q Namespace", name))
			Expect(k8sClient.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: name}})).To(Succeed())

			By(fmt.Sprintf("granting the ServiceAccounts full access to the %q Namespace", name))
			Expect(k8sClient.Create(ctx, &rbacv1.RoleBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name:      uniqueName,
					Namespace: name,
				},
				RoleRef: rbacv1.RoleRef{
					APIGroup: rbacv1.GroupName,
					Kind:     "ClusterRole",
					Name:     "cluster-admin",
				},
				Subjects: []rbacv1.Subject{
					{
						APIGroup: rbacv1.GroupName,
						Kind:     rbacv1.GroupKind,
						Name:     "system:serviceaccounts:" + namespaceName,
					},
				},
			})).To(Succeed())
		}

		By("creating a Workspace whose WorkspaceKind does not set a deletion policy")
		retainWorkspace, retainServiceAccount = createWorkspace(uniqueName+"-retain", nil)

		By("creating a Workspace whose WorkspaceKind has the Delete deletion policy")
		deleteWorkspace, deleteServiceAccount = createWorkspace(uniqueName+"-delete", new(kubefloworgv1beta1.WorkspaceKindCreatedResourcesDeletionPolicyDelete))

		By("creating a ServiceAccount which is named like one of a Workspace, but is not owned by one")
		unownedServiceAccount = &corev1.ServiceAccount{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "ws-" + uniqueName + "-unowned",
				Namespace: namespaceName,
			},
		}
		Expect(k8sClient.Create(ctx, unownedServiceAccount)).To(Succeed())
	})

	AfterAll(func() {
		By("deleting the resources created by the tests")
		Expect(k8sClient.DeleteAllOf(ctx, &corev1.ConfigMap{}, client.InNamespace(namespaceName))).To(Succeed())
		Expect(k8sClient.DeleteAllOf(ctx, &corev1.ConfigMap{}, client.InNamespace(otherNamespaceName))).To(Succeed())

		for _, workspace := range []*kubefloworgv1beta1.Workspace{retainWorkspace, deleteWorkspace} {
			By(fmt.Sprintf("deleting the %q Workspace and WorkspaceKind", workspace.Name))
			Expect(k8sClient.Delete(ctx, workspace)).To(Succeed())
			Expect(k8sClient.Delete(ctx, &kubefloworgv1beta1.WorkspaceKind{
				ObjectMeta: metav1.ObjectMeta{
					Name: workspace.Spec.Kind,
				},
			})).To(Succeed())
		}
	})

	Context("When the ServiceAccount of a Workspace creates a resource", func() {

		It("should label the resource with the Workspace", func() {
			configMap := newConfigMap(namespaceName)
			configMap.Labels = map[string]string{"my-label": "my-value"}
			Expect(newClientFor(retainServiceAccount, retainServiceAccount.UID).Create(ctx, configMap)).To(Succeed())

			Expect(configMap.Labels).To(Equal(map[string]string{
				"my-label":                 "my-value",
				CreatedByWorkspaceLabel:    retainWorkspace.Name,
				CreatedByWorkspaceUIDLabel: string(retainWorkspace.UID),
			}))
		})

		It("should label a resource which has no labels", func() {
			configMap := newConfigMap(namespaceName)
			Expect(newClientFor(retainServiceAccount, retainServiceAccount.UID).Create(ctx, configMap)).To(Succeed())

			Expect(configMap.Labels).To(Equal(map[string]string{
				CreatedByWorkspaceLabel:    retainWorkspace.Name,
				CreatedByWorkspaceUIDLabel: string(retainWorkspace.UID),
			}))
		})

		It("should overwrite labels which claim another Workspace", func() {
			configMap := newConfigMap(namespaceName)
			configMap.Labels = map[string]string{
				CreatedByWorkspaceLabel:    deleteWorkspace.Name,
				CreatedByWorkspaceUIDLabel: string(deleteWorkspace.UID),
			}
			Expect(newClientFor(retainServiceAccount, retainServiceAccount.UID).Create(ctx, configMap)).To(Succeed())

			Expect(configMap.Labels).To(Equal(map[string]string{
				CreatedByWorkspaceLabel:    retainWorkspace.Name,
				CreatedByWorkspaceUIDLabel: string(retainWorkspace.UID),
			}))
		})

		It("should label the resource when the ServiceAccount is impersonated without a UID", func() {
			configMap := newConfigMap(namespaceName)
			Expect(newClientFor(retainServiceAccount, "").Create(ctx, configMap)).To(Succeed())

			Expect(configMap.Labels).To(HaveKeyWithValue(CreatedByWorkspaceUIDLabel, string(retainWorkspace.UID)))
		})

		It("should not make the Workspace an owner by default", func() {
			configMap := newConfigMap(namespaceName)
			Expect(newClientFor(retainServiceAccount, retainServiceAccount.UID).Create(ctx, configMap)).To(Succeed())

			Expect(configMap.Labels).To(HaveKey(CreatedByWorkspaceUIDLabel))
			Expect(configMap.OwnerReferences).To(BeEmpty())
		})

		It("should make the Workspace an owner with the Delete deletion policy", func() {
			otherOwner := metav1.OwnerReference{
				APIVersion: "v1",
				Kind:       "ConfigMap",
				Name:       "other-owner",
				UID:        "00000000-0000-0000-0000-000000000000",
			}

			// NOTE: the webhook reads the Workspace and WorkspaceKind from a cache,
			//       which may not have seen the ones which we have just created
			Eventually(func(g Gomega) {
				configMap := newConfigMap(namespaceName)
				configMap.OwnerReferences = []metav1.OwnerReference{otherOwner}
				g.Expect(newClientFor(deleteServiceAccount, deleteServiceAccount.UID).Create(ctx, configMap)).To(Succeed())

				g.Expect(configMap.Labels).To(HaveKeyWithValue(CreatedByWorkspaceUIDLabel, string(deleteWorkspace.UID)))
				g.Expect(configMap.OwnerReferences).To(Equal([]metav1.OwnerReference{
					otherOwner,
					{
						APIVersion: kubefloworgv1beta1.GroupVersion.String(),
						Kind:       "Workspace",
						Name:       deleteWorkspace.Name,
						UID:        deleteWorkspace.UID,
					},
				}))
			}, time.Second*5, time.Millisecond*100).Should(Succeed())
		})

		It("should not add the Workspace as an owner twice", func() {
			workspaceOwner := metav1.OwnerReference{
				APIVersion: kubefloworgv1beta1.GroupVersion.String(),
				Kind:       "Workspace",
				Name:       deleteWorkspace.Name,
				UID:        deleteWorkspace.UID,
			}
			configMap := newConfigMap(namespaceName)
			configMap.OwnerReferences = []metav1.OwnerReference{workspaceOwner}
			Expect(newClientFor(deleteServiceAccount, deleteServiceAccount.UID).Create(ctx, configMap)).To(Succeed())

			Expect(configMap.OwnerReferences).To(Equal([]metav1.OwnerReference{workspaceOwner}))
		})

		It("should not track a resource in another Namespace", func() {
			configMap := newConfigMap(otherNamespaceName)
			Expect(newClientFor(retainServiceAccount, retainServiceAccount.UID).Create(ctx, configMap)).To(Succeed())

			Expect(configMap.Labels).To(BeEmpty())
			Expect(configMap.OwnerReferences).To(BeEmpty())
		})

		It("should not track resources which are excluded by the webhook configuration", func() {
			serviceAccountClient := newClientFor(retainServiceAccount, retainServiceAccount.UID)

			By("creating an Event")
			event := &corev1.Event{
				ObjectMeta: metav1.ObjectMeta{
					GenerateName: "created-",
					Namespace:    namespaceName,
				},
				InvolvedObject: corev1.ObjectReference{
					Kind:      "ServiceAccount",
					Namespace: namespaceName,
					Name:      retainServiceAccount.Name,
				},
			}
			Expect(serviceAccountClient.Create(ctx, event)).To(Succeed())
			Expect(event.Labels).To(BeEmpty())

			By("creating a Lease")
			lease := &coordinationv1.Lease{
				ObjectMeta: metav1.ObjectMeta{
					GenerateName: "created-",
					Namespace:    namespaceName,
				},
			}
			Expect(serviceAccountClient.Create(ctx, lease)).To(Succeed())
			Expect(lease.Labels).To(BeEmpty())
		})
	})

	Context("When something else creates a resource", func() {

		It("should not track a resource created by a user", func() {
			configMap := newConfigMap(namespaceName)
			Expect(k8sClient.Create(ctx, configMap)).To(Succeed())

			Expect(configMap.Labels).To(BeEmpty())
			Expect(configMap.OwnerReferences).To(BeEmpty())
		})

		It("should not track a resource created by a ServiceAccount which is not owned by a Workspace", func() {
			configMap := newConfigMap(namespaceName)
			Expect(newClientFor(unownedServiceAccount, unownedServiceAccount.UID).Create(ctx, configMap)).To(Succeed())

			Expect(configMap.Labels).To(BeEmpty())
		})

		It("should not track a resource created with the token of a replaced ServiceAccount", func() {
			configMap := newConfigMap(namespaceName)
			Expect(newClientFor(retainServiceAccount, "00000000-0000-0000-0000-000000000000").Create(ctx, configMap)).To(Succeed())

			Expect(configMap.Labels).To(BeEmpty())
		})
	})

	Context("When the request can not be handled", func() {

		// newRequest returns a request to create the given raw resource as a ServiceAccount.
		newRequest := func(operation admissionv1.Operation, serviceAccount *corev1.ServiceAccount, raw string) admission.Request {
			return admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Operation: operation,
					Namespace: serviceAccount.Namespace,
					UserInfo: authenticationv1.UserInfo{
						Username: fmt.Sprintf("system:serviceaccount:%s:%s", serviceAccount.Namespace, serviceAccount.Name),
						UID:      string(serviceAccount.UID),
					},
					Object: runtime.RawExtension{Raw: []byte(raw)},
				},
			}
		}

		It("should allow a resource which it can not decode", func() {
			mutator := &CreatedResourceMutator{Client: k8sClient, APIReader: k8sClient}
			response := mutator.Handle(ctx, newRequest(admissionv1.Create, retainServiceAccount, `{"metadata": {"labels": "invalid"}}`))

			Expect(response.Allowed).To(BeTrue())
			Expect(response.Patches).To(BeEmpty())
		})

		It("should allow an operation which is not a create", func() {
			mutator := &CreatedResourceMutator{Client: k8sClient, APIReader: k8sClient}
			response := mutator.Handle(ctx, newRequest(admissionv1.Update, retainServiceAccount, `{"metadata": {"name": "my-resource"}}`))

			Expect(response.Allowed).To(BeTrue())
			Expect(response.Patches).To(BeEmpty())
		})
	})
})
