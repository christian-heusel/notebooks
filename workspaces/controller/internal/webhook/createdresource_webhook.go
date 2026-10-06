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
	"context"
	"encoding/json"
	"slices"
	"strings"

	"gomodules.xyz/jsonpatch/v2"
	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	kubefloworgv1beta1 "github.com/kubeflow/notebooks/workspaces/controller/api/v1beta1"
	"github.com/kubeflow/notebooks/workspaces/controller/internal/helper"
)

const (
	// CreatedResourceWebhookPath is the path on which the created resource webhook is served
	CreatedResourceWebhookPath = "/mutate-workspace-created-resource"

	// labels which are set on the resources that the ServiceAccount of a Workspace creates
	// NOTE: these are deliberately NOT the "notebooks.kubeflow.org/workspace-name" label, because
	//       the Workspace controller maps every Pod with that label to a reconcile of the Workspace
	CreatedByWorkspaceLabel    = "notebooks.kubeflow.org/created-by-workspace"
	CreatedByWorkspaceUIDLabel = "notebooks.kubeflow.org/created-by-workspace-uid"

	serviceAccountUsernamePrefix = "system:serviceaccount:"
)

// CreatedResourceMutator labels the resources which are created by the ServiceAccount of a Workspace,
// and makes the Workspace their owner if the WorkspaceKind asks for them to be deleted with it.
//
// NOTE: the configuration of this webhook is NOT generated from a `+kubebuilder:webhook` marker,
// because the marker cannot express `matchConditions` or `scope`, it is written by hand in
// `manifests/kustomize/base/webhook/mutating_webhook.yaml`
type CreatedResourceMutator struct {
	client.Client

	// APIReader reads from the API server rather than the cache, it is used to find
	// a ServiceAccount which is too new to be in the cache
	APIReader client.Reader
}

// SetupWebhookWithManager sets up the webhook with the manager
func (m *CreatedResourceMutator) SetupWebhookWithManager(mgr ctrl.Manager) error {
	mgr.GetWebhookServer().Register(CreatedResourceWebhookPath, &webhook.Admission{Handler: m})
	return nil
}

// Handle mutates a resource which is being created by the ServiceAccount of a Workspace.
//
// NOTE: this webhook sees the creation of almost every namespaced resource, so it never denies a request.
// The `failurePolicy` only covers a webhook which can not be called, a response which is not
// allowed is always a denial, so every failure in here results in an unchanged, allowed request.
func (m *CreatedResourceMutator) Handle(ctx context.Context, req admission.Request) admission.Response { //nolint:gocritic
	log := log.FromContext(ctx)

	// the `matchConditions` of the webhook should already have filtered these out,
	// but they are not part of this binary, so we do not rely on them
	if req.Operation != admissionv1.Create || req.SubResource != "" || req.Namespace == "" {
		return admission.Allowed("")
	}
	serviceAccountName, isWorkspaceServiceAccount := workspaceServiceAccountName(req.UserInfo.Username, req.Namespace)
	if !isWorkspaceServiceAccount {
		return admission.Allowed("")
	}

	// fetch the ServiceAccount
	// NOTE: the name alone proves nothing, anybody may create a ServiceAccount named "ws-..."
	serviceAccount := &corev1.ServiceAccount{}
	serviceAccountKey := client.ObjectKey{Namespace: req.Namespace, Name: serviceAccountName}
	if err := m.Get(ctx, serviceAccountKey, serviceAccount); err != nil {
		if !apierrors.IsNotFound(err) {
			log.Error(err, "unable to get ServiceAccount, the created resource is not tracked", "serviceAccount", serviceAccountName)
			return admission.Allowed("")
		}
		// a Workspace may create resources before its ServiceAccount has reached the cache
		if err := m.APIReader.Get(ctx, serviceAccountKey, serviceAccount); err != nil {
			if !apierrors.IsNotFound(err) {
				log.Error(err, "unable to get ServiceAccount, the created resource is not tracked", "serviceAccount", serviceAccountName)
			}
			return admission.Allowed("")
		}
	}

	// the UID is not set when the ServiceAccount is impersonated without one
	if req.UserInfo.UID != "" && req.UserInfo.UID != string(serviceAccount.UID) {
		return admission.Allowed("")
	}

	// the ServiceAccount must belong to a Workspace
	owner := metav1.GetControllerOf(serviceAccount)
	if !helper.IsWorkspaceControllerRef(owner) {
		return admission.Allowed("")
	}

	// we only need the metadata of the resource
	resource := &metav1.PartialObjectMetadata{}
	if err := json.Unmarshal(req.Object.Raw, resource); err != nil {
		log.Error(err, "unable to decode created resource, it is not tracked")
		return admission.Allowed("")
	}

	// label the resource with the Workspace
	// NOTE: the client does not get to choose these labels, so they are overwritten or removed
	labels := resource.Labels
	if labels == nil {
		labels = make(map[string]string)
	}
	labels[CreatedByWorkspaceUIDLabel] = string(owner.UID)
	if len(validation.IsValidLabelValue(owner.Name)) == 0 {
		labels[CreatedByWorkspaceLabel] = owner.Name
	} else {
		// an invalid label would get the resource rejected, the UID label still identifies the Workspace
		delete(labels, CreatedByWorkspaceLabel)
	}
	patches := []jsonpatch.JsonPatchOperation{
		jsonpatch.NewOperation("add", "/metadata/labels", labels),
	}

	// make the Workspace an owner of the resource, so that it is garbage collected with it
	workspace := m.getWorkspaceForDeletion(ctx, req.Namespace, owner)
	if workspace != nil {
		isOwner := slices.ContainsFunc(resource.OwnerReferences, func(ownerReference metav1.OwnerReference) bool {
			return ownerReference.UID == workspace.UID
		})
		if !isOwner {
			resource.OwnerReferences = append(resource.OwnerReferences, metav1.OwnerReference{
				APIVersion: kubefloworgv1beta1.GroupVersion.String(),
				Kind:       helper.OwnerKindWorkspace,
				Name:       workspace.Name,
				UID:        workspace.UID,
				// NOTE: `blockOwnerDeletion` is left unset, it requires the ServiceAccount to have the
				//       permission to update the finalizers of the Workspace, and we do not need it
			})
			patches = append(patches, jsonpatch.NewOperation("add", "/metadata/ownerReferences", resource.OwnerReferences))
		}
	}

	log.V(1).Info("tracking resource created by Workspace",
		"workspace", owner.Name, "kind", req.Kind.Kind, "resource", req.Name, "deletedWithWorkspace", workspace != nil)
	return admission.Patched("", patches...)
}

// getWorkspaceForDeletion returns the Workspace which the owner reference points to if resources
// created by its ServiceAccount should be deleted with it, and nil if they should be retained.
func (m *CreatedResourceMutator) getWorkspaceForDeletion(ctx context.Context, namespace string, owner *metav1.OwnerReference) *kubefloworgv1beta1.Workspace {
	log := log.FromContext(ctx)

	workspace := &kubefloworgv1beta1.Workspace{}
	if err := m.Get(ctx, client.ObjectKey{Namespace: namespace, Name: owner.Name}, workspace); err != nil {
		if !apierrors.IsNotFound(err) {
			log.Error(err, "unable to get Workspace, the created resource is retained", "workspace", owner.Name)
		}
		return nil
	}
	// NOTE: an owner reference with a UID that does not exist makes the garbage collector delete
	//       the resource right away, so we only ever reference the Workspace which we have just seen
	if workspace.UID != owner.UID {
		return nil
	}

	workspaceKind := &kubefloworgv1beta1.WorkspaceKind{}
	if err := m.Get(ctx, client.ObjectKey{Name: workspace.Spec.Kind}, workspaceKind); err != nil {
		if !apierrors.IsNotFound(err) {
			log.Error(err, "unable to get WorkspaceKind, the created resource is retained", "workspaceKind", workspace.Spec.Kind)
		}
		return nil
	}

	serviceAccountConfig := workspaceKind.Spec.PodTemplate.ServiceAccount
	if serviceAccountConfig == nil || serviceAccountConfig.CreatedResources == nil || serviceAccountConfig.CreatedResources.DeletionPolicy == nil {
		return nil
	}
	if *serviceAccountConfig.CreatedResources.DeletionPolicy != kubefloworgv1beta1.WorkspaceKindCreatedResourcesDeletionPolicyDelete {
		return nil
	}
	return workspace
}

// workspaceServiceAccountName returns the name of the ServiceAccount which a username belongs to,
// if it is named like the ServiceAccount of a Workspace and is in the given Namespace.
func workspaceServiceAccountName(username, namespace string) (string, bool) {
	// the username of a ServiceAccount is "system:serviceaccount:{NAMESPACE}:{NAME}"
	name, found := strings.CutPrefix(username, serviceAccountUsernamePrefix+namespace+":")
	if !found || !strings.HasPrefix(name, helper.WorkspaceServiceAccountNamePrefix) {
		return "", false
	}
	return name, true
}
