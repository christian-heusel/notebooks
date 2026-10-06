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
	kubefloworgv1beta1 "github.com/kubeflow/notebooks/workspaces/controller/api/v1beta1"

	"github.com/kubeflow/notebooks/workspaces/backend/internal/models/common"
)

// CreatedResource represents a resource which was created by the ServiceAccount of a Workspace.
type CreatedResource struct {
	// Group is the API group of the resource, it is empty for the core API group.
	Group string `json:"group"`

	// Version is the API version of the resource.
	Version string `json:"version"`

	// Resource is the plural name of the kind of the resource, as used in URLs of the Kubernetes API.
	Resource string `json:"resource"`

	// Kind is the kind of the resource.
	Kind string `json:"kind"`

	// Name is the name of the resource.
	Name string `json:"name"`

	// DeletionPolicy is "Delete" if the resource is owned by the Workspace, and so is deleted with it.
	DeletionPolicy kubefloworgv1beta1.WorkspaceKindCreatedResourcesDeletionPolicy `json:"deletionPolicy"`

	Audit common.Audit `json:"audit"`
}
