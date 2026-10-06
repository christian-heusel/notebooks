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
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	kubefloworgv1beta1 "github.com/kubeflow/notebooks/workspaces/controller/api/v1beta1"
	authorizationv1 "k8s.io/api/authorization/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/cache"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/metadata"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kubeflow/notebooks/workspaces/backend/internal/config"
	modelsCommon "github.com/kubeflow/notebooks/workspaces/backend/internal/models/common"
	models "github.com/kubeflow/notebooks/workspaces/backend/internal/models/workspaces/createdresources"
	repoCommon "github.com/kubeflow/notebooks/workspaces/backend/internal/repositories/common"
)

const (
	// TTL for the kinds of resources which the cluster serves.
	discoveredResourcesTTL = 60 * time.Second

	// TTL for the kinds of resources which the backend is allowed to list in a Namespace.
	listableResourcesTTL = 60 * time.Second

	// Maximum entries in the listable resources LRU cache.
	listableResourcesCacheMaxCapacity = 1000

	// TTL for the created resources of a Workspace.
	createdResourcesTTL = 10 * time.Second

	// Maximum entries in the created resources LRU cache.
	createdResourcesCacheMaxCapacity = 10000

	// Timeout for listing the resources of a single kind.
	listTimeout = 5 * time.Second

	// Maximum number of kinds of resources which are listed at the same time for a request.
	listConcurrency = 10
)

// excludedGroups are the API groups whose resources are never listed, because they are derived from
// other resources (a PodMetrics has the labels of its Pod), or because the controller does not track them.
var excludedGroups = []string{
	"authentication.k8s.io",
	"authorization.k8s.io",
	"coordination.k8s.io",
	"custom.metrics.k8s.io",
	"events.k8s.io",
	"external.metrics.k8s.io",
	"metrics.k8s.io",
}

// excludedCoreResources are the resources of the core API group which the controller does not track.
var excludedCoreResources = []string{
	"bindings",
	"events",
}

// listableResource is a kind of resource which can be listed in a Namespace.
type listableResource struct {
	gvr  schema.GroupVersionResource
	kind string
}

// CreatedResourcesRepository finds the resources which were created by the ServiceAccount of a Workspace.
//
// The controller labels these resources as they are created, but Kubernetes can not list resources across
// kinds, so each kind is listed on its own. To keep that cheap, and to not need access to every resource of
// the cluster, only the kinds which the backend is allowed to list are considered. There are three caches:
//  1. Discovered resources (discovered): the namespaced kinds which the cluster serves (60s TTL).
//  2. Listable resources (listableCache): the discovered kinds which the backend is allowed to list
//     in a Namespace, as reported by a SelfSubjectRulesReview (60s TTL, keyed by Namespace).
//  3. Created resources (createdResourcesCache): the result for a Workspace (10s TTL, keyed by
//     "<namespace>/<workspace UID>"), which is shared by all users, so it is filtered afterward.
type CreatedResourcesRepository struct {
	cfg            *config.EnvConfig
	client         client.Client
	clientset      kubernetes.Interface
	metadataClient metadata.Interface
	logger         *slog.Logger

	discoveredMutex     sync.Mutex
	discovered          []listableResource
	discoveredExpiresAt time.Time

	listableCache         *cache.LRUExpireCache
	createdResourcesCache *cache.LRUExpireCache
}

// NewCreatedResourcesRepository creates a CreatedResourcesRepository.
func NewCreatedResourcesRepository(
	cfg *config.EnvConfig,
	cl client.Client,
	clientset kubernetes.Interface,
	metadataClient metadata.Interface,
	logger *slog.Logger,
) *CreatedResourcesRepository {
	if logger == nil {
		logger = slog.Default()
	}
	return &CreatedResourcesRepository{
		cfg:                   cfg,
		client:                cl,
		clientset:             clientset,
		metadataClient:        metadataClient,
		logger:                logger,
		listableCache:         cache.NewLRUExpireCache(listableResourcesCacheMaxCapacity),
		createdResourcesCache: cache.NewLRUExpireCache(createdResourcesCacheMaxCapacity),
	}
}

// GetCreatedResources returns the resources which were created by the ServiceAccount of a Workspace,
// sorted by group, kind, and name. Kinds of resources which the backend is not allowed to list are not included.
func (r *CreatedResourcesRepository) GetCreatedResources(ctx context.Context, namespace, workspaceName string) ([]models.CreatedResource, error) {
	if r.clientset == nil || r.metadataClient == nil {
		return nil, errors.New("created resources repository is not configured")
	}

	workspace := &kubefloworgv1beta1.Workspace{}
	if err := r.client.Get(ctx, client.ObjectKey{Namespace: namespace, Name: workspaceName}, workspace); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, repoCommon.ErrWorkspaceNotFound
		}
		return nil, err
	}

	// NOTE: the UID is part of the key, so a Workspace never gets the result of an older one with the same name
	cacheKey := fmt.Sprintf("%s/%s", namespace, workspace.UID)
	if val, ok := r.createdResourcesCache.Get(cacheKey); ok {
		if createdResources, valid := val.([]models.CreatedResource); valid {
			return createdResources, nil
		}
	}

	listableResources, err := r.getListableResources(ctx, namespace)
	if err != nil {
		return nil, err
	}

	// NOTE: we select by the UID rather than the name of the Workspace, because resources
	//       which were retained from a deleted Workspace may have the same name label
	selector := labels.SelectorFromSet(labels.Set{modelsCommon.LabelCreatedByWorkspaceUID: string(workspace.UID)}).String()

	// list each kind of resource, a few at a time
	var (
		waitGroup  sync.WaitGroup
		incomplete atomic.Bool
	)
	results := make([][]models.CreatedResource, len(listableResources))
	semaphore := make(chan struct{}, listConcurrency)
	for i, resource := range listableResources {
		waitGroup.Go(func() {
			semaphore <- struct{}{}
			defer func() { <-semaphore }()

			listCtx, cancel := context.WithTimeout(ctx, listTimeout)
			defer cancel()

			// NOTE: we only need the metadata of the resources, which is also much cheaper to list
			list, err := r.metadataClient.Resource(resource.gvr).Namespace(namespace).List(listCtx, metav1.ListOptions{LabelSelector: selector})
			if err != nil {
				// a kind which can not be listed should not hide the resources of the other kinds
				incomplete.Store(true)
				r.logger.Warn("failed to list created resources",
					"namespace", namespace, "workspace", workspaceName, "resource", resource.gvr.String(), "error", err)
				return
			}
			for j := range list.Items {
				results[i] = append(results[i], models.NewCreatedResourceFromObjectMeta(resource.gvr, resource.kind, &list.Items[j].ObjectMeta, workspace.UID))
			}
		})
	}
	waitGroup.Wait()
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	createdResources := slices.Concat(results...)
	if createdResources == nil {
		createdResources = []models.CreatedResource{}
	}
	models.SortCreatedResources(createdResources)

	// a result with missing kinds is not cached, so the next request tries them again
	if !incomplete.Load() {
		r.createdResourcesCache.Add(cacheKey, createdResources, createdResourcesTTL)
	}
	return createdResources, nil
}

// getListableResources returns the kinds of resources which the backend is allowed to list in a Namespace.
func (r *CreatedResourcesRepository) getListableResources(ctx context.Context, namespace string) ([]listableResource, error) {
	if val, ok := r.listableCache.Get(namespace); ok {
		if listableResources, valid := val.([]listableResource); valid {
			return listableResources, nil
		}
	}

	discovered, err := r.getDiscoveredResources()
	if err != nil {
		return nil, err
	}

	// ask the API server for the permissions of the backend, rather than trying to list every kind
	review := &authorizationv1.SelfSubjectRulesReview{
		Spec: authorizationv1.SelfSubjectRulesReviewSpec{Namespace: namespace},
	}
	review, err = r.clientset.AuthorizationV1().SelfSubjectRulesReviews().Create(ctx, review, metav1.CreateOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to review the permissions of the backend in namespace %q: %w", namespace, err)
	}

	listableResources := make([]listableResource, 0, len(discovered))
	for _, resource := range discovered {
		if rulesAllowList(review.Status.ResourceRules, resource.gvr) {
			listableResources = append(listableResources, resource)
		}
	}

	r.listableCache.Add(namespace, listableResources, listableResourcesTTL)
	return listableResources, nil
}

// getDiscoveredResources returns the namespaced kinds of resources which the cluster serves and can be listed.
func (r *CreatedResourcesRepository) getDiscoveredResources() ([]listableResource, error) {
	r.discoveredMutex.Lock()
	defer r.discoveredMutex.Unlock()

	if time.Now().Before(r.discoveredExpiresAt) {
		return r.discovered, nil
	}

	// NOTE: discovery fails for a group whose aggregated API server is unavailable,
	//       the other groups are still returned and are all we can use
	resourceLists, err := discovery.ServerPreferredNamespacedResources(r.clientset.Discovery())
	if err != nil && !discovery.IsGroupDiscoveryFailedError(err) {
		return nil, fmt.Errorf("failed to discover the resources of the cluster: %w", err)
	}
	resourceLists = discovery.FilteredBy(discovery.SupportsAllVerbs{Verbs: []string{"list"}}, resourceLists)

	var discovered []listableResource
	for _, resourceList := range resourceLists {
		groupVersion, err := schema.ParseGroupVersion(resourceList.GroupVersion)
		if err != nil {
			continue
		}
		if slices.Contains(excludedGroups, groupVersion.Group) {
			continue
		}
		for i := range resourceList.APIResources {
			resource := &resourceList.APIResources[i]
			// subresources are not resources of their own
			if strings.Contains(resource.Name, "/") {
				continue
			}
			if groupVersion.Group == "" && slices.Contains(excludedCoreResources, resource.Name) {
				continue
			}
			discovered = append(discovered, listableResource{
				gvr:  groupVersion.WithResource(resource.Name),
				kind: resource.Kind,
			})
		}
	}

	r.discovered = discovered
	r.discoveredExpiresAt = time.Now().Add(discoveredResourcesTTL)
	return discovered, nil
}

// rulesAllowList reports whether the rules allow all resources of the given kind to be listed.
func rulesAllowList(rules []authorizationv1.ResourceRule, gvr schema.GroupVersionResource) bool {
	for i := range rules {
		rule := &rules[i]
		// a rule for specific resource names does not allow a list
		if len(rule.ResourceNames) > 0 {
			continue
		}
		if ruleMatches(rule.Verbs, rbacv1.VerbAll, "list") &&
			ruleMatches(rule.APIGroups, rbacv1.APIGroupAll, gvr.Group) &&
			ruleMatches(rule.Resources, rbacv1.ResourceAll, gvr.Resource) {
			return true
		}
	}
	return false
}

// ruleMatches reports whether a field of a rule contains the wildcard or the given value.
func ruleMatches(values []string, wildcard, value string) bool {
	return slices.Contains(values, wildcard) || slices.Contains(values, value)
}
