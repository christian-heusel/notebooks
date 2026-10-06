import { mockModArchResponse } from 'mod-arch-core';
import {
  deleteModal,
  workspaceDetailsDrawer,
  workspaces,
} from '~/__tests__/cypress/cypress/pages/workspaces/workspaces';
import {
  buildMockCreatedResource,
  buildMockNamespace,
  buildMockWorkspace,
  buildMockWorkspaceCreatedResources,
  buildMockWorkspaceDetails,
} from '~/shared/mock/mockBuilder';
import { NOTEBOOKS_API_VERSION } from '~/__tests__/cypress/cypress/support/commands/api';
import { navBar } from '~/__tests__/cypress/cypress/pages/components/navBar';
import type { ApiCreatedResourceListEnvelope, ApiErrorEnvelope } from '~/generated/data-contracts';
import { V1Beta1WorkspaceKindCreatedResourcesDeletionPolicy } from '~/generated/data-contracts';

const DEFAULT_NAMESPACE = 'default';
const TEST_WORKSPACE_NAME = 'TestWorkspace';

const setupWorkspace = (
  createdResourcesResponse: ApiCreatedResourceListEnvelope | ApiErrorEnvelope,
) => {
  const mockNamespace = buildMockNamespace({ name: DEFAULT_NAMESPACE });
  const mockWorkspace = buildMockWorkspace({
    name: TEST_WORKSPACE_NAME,
    namespace: DEFAULT_NAMESPACE,
  });

  cy.interceptApi(
    'GET /api/:apiVersion/namespaces',
    { path: { apiVersion: NOTEBOOKS_API_VERSION } },
    mockModArchResponse([mockNamespace]),
  ).as('getNamespaces');

  cy.interceptApi(
    'GET /api/:apiVersion/workspaces/:namespace',
    { path: { apiVersion: NOTEBOOKS_API_VERSION, namespace: DEFAULT_NAMESPACE } },
    mockModArchResponse([mockWorkspace]),
  ).as('getWorkspaces');

  cy.interceptApi(
    'GET /api/:apiVersion/workspaces/:namespace/:workspaceName/podtemplate/details',
    {
      path: {
        apiVersion: NOTEBOOKS_API_VERSION,
        namespace: DEFAULT_NAMESPACE,
        workspaceName: TEST_WORKSPACE_NAME,
      },
    },
    mockModArchResponse(buildMockWorkspaceDetails()),
  ).as('getWorkspaceDetails');

  cy.interceptApi(
    'GET /api/:apiVersion/workspaces/:namespace/:workspaceName/createdresources',
    {
      path: {
        apiVersion: NOTEBOOKS_API_VERSION,
        namespace: DEFAULT_NAMESPACE,
        workspaceName: TEST_WORKSPACE_NAME,
      },
    },
    createdResourcesResponse,
  ).as('getCreatedResources');

  workspaces.visit();
  cy.wait('@getNamespaces');
  navBar.selectNamespace(DEFAULT_NAMESPACE);
  cy.wait('@getWorkspaces');
};

const openCreatedResourcesTab = () => {
  workspaces.findAction({ action: 'viewDetails', workspaceName: TEST_WORKSPACE_NAME }).click();
  workspaceDetailsDrawer.findCreatedResourcesTab().click();
  cy.wait('@getCreatedResources');
};

describe('Workspace Created Resources Tab', () => {
  it('should only request the created resources once the tab is opened', () => {
    setupWorkspace(mockModArchResponse(buildMockWorkspaceCreatedResources()));

    workspaces.findAction({ action: 'viewDetails', workspaceName: TEST_WORKSPACE_NAME }).click();
    workspaceDetailsDrawer.findOverviewTab().should('exist');
    cy.get('@getCreatedResources.all').should('have.length', 0);

    workspaceDetailsDrawer.findCreatedResourcesTab().click();
    cy.wait('@getCreatedResources');
  });

  it('should list the resources created by the workspace', () => {
    setupWorkspace(mockModArchResponse(buildMockWorkspaceCreatedResources()));
    openCreatedResourcesTab();

    workspaceDetailsDrawer
      .findCreatedResource('ConfigMap', 'my-config')
      .should('contain.text', 'ConfigMap')
      .and('contain.text', 'my-config')
      .and('contain.text', 'Retained');
    workspaceDetailsDrawer
      .findCreatedResource('PersistentVolumeClaim', 'my-dataset')
      .should('contain.text', 'Retained');
    workspaceDetailsDrawer
      .findCreatedResource('TrainJob', 'my-train-job')
      .should('contain.text', 'trainer.kubeflow.org/v1alpha1')
      .and('contain.text', 'Deleted');
  });

  it('should show an empty state when the workspace has not created any resources', () => {
    setupWorkspace(mockModArchResponse([]));
    openCreatedResourcesTab();

    workspaceDetailsDrawer
      .findCreatedResourcesTabContent()
      .findByTestId('created-resources-empty')
      .should('exist');
  });

  it('should show an error when the created resources fail to load', () => {
    setupWorkspace({ error: { code: '500', message: 'Internal server error.' } });
    openCreatedResourcesTab();

    workspaceDetailsDrawer
      .findCreatedResourcesTabContent()
      .findByTestId('details-loading-error')
      .should('exist');
  });
});

describe('Workspace Delete Modal', () => {
  it('should warn about the created resources which are deleted with the workspace', () => {
    setupWorkspace(
      mockModArchResponse([
        buildMockCreatedResource({ name: 'retained-job' }),
        buildMockCreatedResource({
          name: 'deleted-job',
          deletionPolicy:
            V1Beta1WorkspaceKindCreatedResourcesDeletionPolicy.WorkspaceKindCreatedResourcesDeletionPolicyDelete,
        }),
      ]),
    );

    workspaces.findAction({ action: 'delete', workspaceName: TEST_WORKSPACE_NAME }).click();
    cy.wait('@getCreatedResources');

    deleteModal
      .findCreatedResourcesWarning()
      .should('contain.text', '1 resource created by this workspace will also be deleted.');
  });

  it('should not warn when no created resource is deleted with the workspace', () => {
    setupWorkspace(mockModArchResponse([buildMockCreatedResource({ name: 'retained-job' })]));

    workspaces.findAction({ action: 'delete', workspaceName: TEST_WORKSPACE_NAME }).click();
    cy.wait('@getCreatedResources');

    deleteModal.assertModalExists();
    deleteModal.findCreatedResourcesWarning().should('not.exist');
  });
});
