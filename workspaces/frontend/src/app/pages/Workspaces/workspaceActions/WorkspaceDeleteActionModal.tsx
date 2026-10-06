import React from 'react';
import { Alert } from '@patternfly/react-core/dist/esm/components/Alert';
import { Stack, StackItem } from '@patternfly/react-core/dist/esm/layouts/Stack';
import { useWorkspaceCreatedResources } from '~/app/hooks/useWorkspaceCreatedResources';
import DeleteModal from '~/shared/components/DeleteModal';
import {
  V1Beta1WorkspaceKindCreatedResourcesDeletionPolicy,
  WorkspacesWorkspaceListItem,
} from '~/generated/data-contracts';

interface WorkspaceDeleteActionModalProps {
  workspace: WorkspacesWorkspaceListItem;
  onClose: () => void;
  onDelete: () => Promise<void>;
}

const formatResourceCount = (count: number): string =>
  `${count} ${count === 1 ? 'resource' : 'resources'}`;

export const WorkspaceDeleteActionModal: React.FC<WorkspaceDeleteActionModalProps> = ({
  workspace,
  onClose,
  onDelete,
}) => {
  // NOTE: if the created resources fail to load, the workspace can still be deleted, just without the warnings
  const [createdResources] = useWorkspaceCreatedResources(workspace.namespace, workspace.name);
  const deletedCount = createdResources.filter(
    (createdResource) =>
      createdResource.deletionPolicy ===
      V1Beta1WorkspaceKindCreatedResourcesDeletionPolicy.WorkspaceKindCreatedResourcesDeletionPolicyDelete,
  ).length;
  const retainedCount = createdResources.length - deletedCount;

  return (
    <DeleteModal
      isOpen
      resourceName={workspace.name}
      namespace={workspace.namespace}
      title="Delete workspace?"
      onClose={onClose}
      onDelete={onDelete}
      message={
        createdResources.length > 0 ? (
          <Stack hasGutter>
            <StackItem>
              Are you sure you want to delete <strong>{workspace.name}</strong> in namespace{' '}
              <strong>{workspace.namespace}</strong>?
            </StackItem>
            {deletedCount > 0 && (
              <StackItem>
                <Alert
                  variant="warning"
                  isInline
                  isPlain
                  title={`${formatResourceCount(deletedCount)} created by this workspace will also be deleted.`}
                  data-testid="delete-modal-deleted-resources-warning"
                />
              </StackItem>
            )}
            {retainedCount > 0 && (
              <StackItem>
                <Alert
                  variant="warning"
                  isInline
                  isPlain
                  title={`${formatResourceCount(retainedCount)} created by this workspace will not be deleted and will remain in the namespace.`}
                  data-testid="delete-modal-retained-resources-warning"
                />
              </StackItem>
            )}
          </Stack>
        ) : undefined
      }
    />
  );
};
