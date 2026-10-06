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
	"cmp"
	"slices"

	kubefloworgv1beta1 "github.com/kubeflow/notebooks/workspaces/controller/api/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"

	"github.com/kubeflow/notebooks/workspaces/backend/internal/models/common"
)

// NewCreatedResourceFromObjectMeta creates a CreatedResource from the metadata of a resource
// which was created by the ServiceAccount of the Workspace with the given UID.
func NewCreatedResourceFromObjectMeta(gvr schema.GroupVersionResource, kind string, objectMeta *metav1.ObjectMeta, workspaceUID types.UID) CreatedResource {
	// the controller makes the Workspace an owner of the resources which are deleted with it
	deletionPolicy := kubefloworgv1beta1.WorkspaceKindCreatedResourcesDeletionPolicyRetain
	for _, ownerReference := range objectMeta.OwnerReferences {
		if ownerReference.UID == workspaceUID {
			deletionPolicy = kubefloworgv1beta1.WorkspaceKindCreatedResourcesDeletionPolicyDelete
			break
		}
	}

	return CreatedResource{
		Group:          gvr.Group,
		Version:        gvr.Version,
		Resource:       gvr.Resource,
		Kind:           kind,
		Name:           objectMeta.Name,
		DeletionPolicy: deletionPolicy,
		Audit:          common.NewAuditFromObjectMeta(objectMeta),
	}
}

// SortCreatedResources sorts the resources by group, kind, and name.
func SortCreatedResources(createdResources []CreatedResource) {
	slices.SortFunc(createdResources, func(a, b CreatedResource) int {
		return cmp.Or(
			cmp.Compare(a.Group, b.Group),
			cmp.Compare(a.Kind, b.Kind),
			cmp.Compare(a.Name, b.Name),
		)
	})
}
