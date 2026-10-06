import React from 'react';
import { render, screen } from '@testing-library/react';
import '@testing-library/jest-dom';
import { WorkspaceDeleteActionModal } from '~/app/pages/Workspaces/workspaceActions/WorkspaceDeleteActionModal';
import { useWorkspaceCreatedResources } from '~/app/hooks/useWorkspaceCreatedResources';
import {
  CreatedresourcesCreatedResource,
  V1Beta1WorkspaceKindCreatedResourcesDeletionPolicy,
} from '~/generated/data-contracts';
import { buildMockCreatedResource, buildMockWorkspace } from '~/shared/mock/mockBuilder';

jest.mock('~/app/hooks/useWorkspaceCreatedResources', () => ({
  useWorkspaceCreatedResources: jest.fn(),
}));

const mockUseWorkspaceCreatedResources = useWorkspaceCreatedResources as jest.MockedFunction<
  typeof useWorkspaceCreatedResources
>;

const mockWorkspace = buildMockWorkspace({ name: 'test-workspace', namespace: 'test-ns' });

const deletedResource = (name: string) =>
  buildMockCreatedResource({
    name,
    deletionPolicy:
      V1Beta1WorkspaceKindCreatedResourcesDeletionPolicy.WorkspaceKindCreatedResourcesDeletionPolicyDelete,
  });

const renderModal = (
  createdResources: CreatedresourcesCreatedResource[],
  loaded = true,
  error?: Error,
) => {
  mockUseWorkspaceCreatedResources.mockReturnValue([createdResources, loaded, error, jest.fn()]);
  return render(
    <WorkspaceDeleteActionModal
      workspace={mockWorkspace}
      onClose={jest.fn()}
      onDelete={jest.fn()}
    />,
  );
};

describe('WorkspaceDeleteActionModal', () => {
  beforeEach(() => {
    jest.clearAllMocks();
  });

  it('asks to confirm the deletion of the workspace', () => {
    renderModal([]);

    expect(mockUseWorkspaceCreatedResources).toHaveBeenCalledWith('test-ns', 'test-workspace');
    expect(screen.getByTestId('delete-modal')).toBeInTheDocument();
    expect(screen.getByText('test-workspace')).toBeInTheDocument();
    expect(screen.getByText('test-ns')).toBeInTheDocument();
  });

  it('does not warn when the workspace has not created any resources', () => {
    renderModal([]);

    expect(screen.queryByTestId('delete-modal-deleted-resources-warning')).not.toBeInTheDocument();
    expect(screen.queryByTestId('delete-modal-retained-resources-warning')).not.toBeInTheDocument();
  });

  it('warns about the created resources which are deleted with the workspace', () => {
    renderModal([deletedResource('job-1'), deletedResource('job-2')]);

    expect(screen.getByTestId('delete-modal-deleted-resources-warning')).toHaveTextContent(
      '2 resources created by this workspace will also be deleted.',
    );
    expect(screen.queryByTestId('delete-modal-retained-resources-warning')).not.toBeInTheDocument();
    expect(screen.getByText('test-workspace')).toBeInTheDocument();
  });

  it('warns about the created resources which are retained', () => {
    renderModal([buildMockCreatedResource({ name: 'job-1' })]);

    expect(screen.getByTestId('delete-modal-retained-resources-warning')).toHaveTextContent(
      '1 resource created by this workspace will not be deleted and will remain in the namespace.',
    );
    expect(screen.queryByTestId('delete-modal-deleted-resources-warning')).not.toBeInTheDocument();
    expect(screen.getByText('test-workspace')).toBeInTheDocument();
  });

  it('warns about both the deleted and the retained resources', () => {
    renderModal([
      buildMockCreatedResource({ name: 'job-1' }),
      buildMockCreatedResource({ name: 'job-2' }),
      deletedResource('job-3'),
    ]);

    expect(screen.getByTestId('delete-modal-deleted-resources-warning')).toHaveTextContent(
      '1 resource created by this workspace will also be deleted.',
    );
    expect(screen.getByTestId('delete-modal-retained-resources-warning')).toHaveTextContent(
      '2 resources created by this workspace will not be deleted and will remain in the namespace.',
    );
  });

  it('still allows the deletion when the created resources failed to load', () => {
    renderModal([], false, new Error('boom'));

    expect(screen.getByTestId('delete-modal')).toBeInTheDocument();
    expect(screen.queryByTestId('delete-modal-deleted-resources-warning')).not.toBeInTheDocument();
    expect(screen.queryByTestId('delete-modal-retained-resources-warning')).not.toBeInTheDocument();
  });
});
