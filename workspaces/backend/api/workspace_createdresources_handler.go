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
	"errors"
	"net/http"

	"github.com/julienschmidt/httprouter"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation/field"

	"github.com/kubeflow/notebooks/workspaces/backend/api/constants"
	"github.com/kubeflow/notebooks/workspaces/backend/internal/auth"
	"github.com/kubeflow/notebooks/workspaces/backend/internal/helper"
	models "github.com/kubeflow/notebooks/workspaces/backend/internal/models/workspaces/createdresources"
	repoCommon "github.com/kubeflow/notebooks/workspaces/backend/internal/repositories/common"
)

// CreatedResourceListEnvelope is the response envelope for the resources created by a workspace.
type CreatedResourceListEnvelope Envelope[[]models.CreatedResource]

// GetWorkspaceCreatedResourcesHandler returns the resources which were created by a workspace.
//
//	@Summary		List resources created by workspace
//	@Description	Returns the resources which were created by the ServiceAccount of the workspace, in the namespace of the workspace. Only kinds of resources which both the backend and the user are allowed to list are returned.
//	@Tags			workspaces
//	@ID				listWorkspaceCreatedResources
//	@Produce		json
//	@Param			namespace	path		string						true	"Namespace of the workspace"	extensions(x-example=kubeflow-user-example-com)
//	@Param			name		path		string						true	"Name of the workspace"			extensions(x-example=my-workspace)
//	@Success		200			{object}	CreatedResourceListEnvelope	"Successful operation. Returns the resources created by the workspace."
//	@Failure		401			{object}	ErrorEnvelope				"Unauthorized."
//	@Failure		403			{object}	ErrorEnvelope				"Forbidden."
//	@Failure		404			{object}	ErrorEnvelope				"Workspace not found."
//	@Failure		422			{object}	ErrorEnvelope				"Unprocessable Entity. Validation error."
//	@Failure		500			{object}	ErrorEnvelope				"Internal server error."
//	@Router			/workspaces/{namespace}/{name}/createdresources [get]
func (a *App) GetWorkspaceCreatedResourcesHandler(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	namespace := ps.ByName(constants.NamespacePathParam)
	workspaceName := ps.ByName(constants.ResourceNamePathParam)

	// validate path parameters
	var valErrs field.ErrorList //nolint:prealloc
	valErrs = append(valErrs, helper.ValidateKubernetesNamespaceName(field.NewPath(constants.NamespacePathParam), namespace)...)
	valErrs = append(valErrs, helper.ValidateWorkspaceName(field.NewPath(constants.ResourceNamePathParam), workspaceName)...)
	if len(valErrs) > 0 {
		a.failedValidationResponse(w, r, errMsgPathParamsInvalid, valErrs, nil)
		return
	}

	// =========================== AUTH ===========================
	authPolicies := []*auth.ResourcePolicy{
		auth.NewResourcePolicy(auth.VerbGet, auth.Workspaces, auth.ResourcePolicyResourceMeta{Namespace: namespace, Name: workspaceName}),
	}
	user, ok := a.requireAuth(w, r, authPolicies)
	if !ok {
		return
	}
	// ============================================================

	createdResources, err := a.repositories.CreatedResources.GetCreatedResources(r.Context(), namespace, workspaceName)
	if err != nil {
		if errors.Is(err, repoCommon.ErrWorkspaceNotFound) {
			a.notFoundResponse(w, r)
			return
		}
		a.serverErrorResponse(w, r, err)
		return
	}

	// the resources were listed by the backend, so only return the kinds which the user may list themselves
	// NOTE: the kind of the resources is not known in advance, so this can not be an auth policy of the request
	canList := make(map[schema.GroupVersionResource]bool)
	visibleResources := make([]models.CreatedResource, 0, len(createdResources))
	for i := range createdResources {
		createdResource := &createdResources[i]
		gvr := schema.GroupVersionResource{Group: createdResource.Group, Version: createdResource.Version, Resource: createdResource.Resource}
		allowed, checked := canList[gvr]
		if !checked {
			policy := &auth.ResourcePolicy{
				Verb:         auth.VerbList,
				GVR:          gvr,
				ResourceMeta: auth.ResourcePolicyResourceMeta{Namespace: namespace},
			}
			allowed, err = a.isAuthorized(r.Context(), user, policy)
			if err != nil {
				a.serverErrorResponse(w, r, err)
				return
			}
			canList[gvr] = allowed
		}
		if allowed {
			visibleResources = append(visibleResources, *createdResource)
		}
	}

	responseEnvelope := &CreatedResourceListEnvelope{Data: visibleResources}
	a.dataResponse(w, r, responseEnvelope)
}
