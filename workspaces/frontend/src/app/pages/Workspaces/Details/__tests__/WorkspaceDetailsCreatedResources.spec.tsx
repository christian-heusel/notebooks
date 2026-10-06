import React from 'react';
import { render, screen, within } from '@testing-library/react';
import '@testing-library/jest-dom';
import { WorkspaceDetailsCreatedResources } from '~/app/pages/Workspaces/Details/WorkspaceDetailsCreatedResources';
import { useWorkspaceCreatedResources } from '~/app/hooks/useWorkspaceCreatedResources';
import { CreatedresourcesCreatedResource } from '~/generated/data-contracts';
import { buildMockWorkspace, buildMockWorkspaceCreatedResources } from '~/shared/mock/mockBuilder';

jest.mock('~/app/hooks/useWorkspaceCreatedResources', () => ({
  useWorkspaceCreatedResources: jest.fn(),
}));

const mockUseWorkspaceCreatedResources = useWorkspaceCreatedResources as jest.MockedFunction<
  typeof useWorkspaceCreatedResources
>;

const mockWorkspace = buildMockWorkspace({ name: 'test-workspace', namespace: 'test-ns' });

const renderTab = (
  createdResources: CreatedresourcesCreatedResource[],
  loaded = true,
  error?: Error,
) => {
  mockUseWorkspaceCreatedResources.mockReturnValue([createdResources, loaded, error, jest.fn()]);
  return render(<WorkspaceDetailsCreatedResources workspace={mockWorkspace} />);
};

describe('WorkspaceDetailsCreatedResources', () => {
  beforeEach(() => {
    jest.clearAllMocks();
  });

  it('requests the created resources of the workspace', () => {
    renderTab([]);

    expect(mockUseWorkspaceCreatedResources).toHaveBeenCalledWith('test-ns', 'test-workspace');
  });

  it('shows a spinner while the created resources are loading', () => {
    renderTab([], false);

    expect(screen.getByTestId('details-loading-spinner')).toBeInTheDocument();
  });

  it('shows an error when the created resources failed to load', () => {
    renderTab([], false, new Error('boom'));

    expect(screen.getByTestId('details-loading-error')).toBeInTheDocument();
  });

  it('shows an empty state when the workspace has not created any resources', () => {
    renderTab([]);

    expect(screen.getByTestId('created-resources-empty')).toBeInTheDocument();
    expect(screen.queryByTestId('created-resources-table')).not.toBeInTheDocument();
  });

  it('lists the created resources with their kind, API version, and name', () => {
    renderTab(buildMockWorkspaceCreatedResources());

    const configMapRow = screen.getByTestId('created-resource-ConfigMap-my-config');
    expect(within(configMapRow).getByText('ConfigMap')).toBeInTheDocument();
    expect(within(configMapRow).getByText('v1')).toBeInTheDocument();
    expect(within(configMapRow).getByText('my-config')).toBeInTheDocument();

    const trainJobRow = screen.getByTestId('created-resource-TrainJob-my-train-job');
    expect(within(trainJobRow).getByText('TrainJob')).toBeInTheDocument();
    expect(within(trainJobRow).getByText('trainer.kubeflow.org/v1alpha1')).toBeInTheDocument();
  });

  it('shows whether a created resource is deleted with the workspace', () => {
    renderTab(buildMockWorkspaceCreatedResources());

    const retainedRow = screen.getByTestId('created-resource-ConfigMap-my-config');
    expect(within(retainedRow).getByText('Retained')).toBeInTheDocument();

    const deletedRow = screen.getByTestId('created-resource-TrainJob-my-train-job');
    expect(within(deletedRow).getByText('Deleted')).toBeInTheDocument();
  });
});
