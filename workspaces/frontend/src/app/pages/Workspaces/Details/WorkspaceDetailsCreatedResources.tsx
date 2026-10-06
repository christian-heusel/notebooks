import React from 'react';
import { format } from 'date-fns/format';
import { Table, Thead, Tr, Td, Tbody, Th } from '@patternfly/react-table/dist/esm/components/Table';
import { Content } from '@patternfly/react-core/dist/esm/components/Content';
import { EmptyState, EmptyStateBody } from '@patternfly/react-core/dist/esm/components/EmptyState';
import { Label } from '@patternfly/react-core/dist/esm/components/Label';
import { CubesIcon } from '@patternfly/react-icons/dist/esm/icons/cubes-icon';
import { DetailsLoadingState } from '~/app/components/DetailsLoadingState';
import { useWorkspaceCreatedResources } from '~/app/hooks/useWorkspaceCreatedResources';
import {
  V1Beta1WorkspaceKindCreatedResourcesDeletionPolicy,
  WorkspacesWorkspaceListItem,
} from '~/generated/data-contracts';

const DATE_FORMAT = 'PPp';

type WorkspaceDetailsCreatedResourcesProps = {
  workspace: WorkspacesWorkspaceListItem;
};

export const WorkspaceDetailsCreatedResources: React.FunctionComponent<
  WorkspaceDetailsCreatedResourcesProps
> = ({ workspace }) => {
  const [createdResources, loaded, error] = useWorkspaceCreatedResources(
    workspace.namespace,
    workspace.name,
  );

  return (
    <DetailsLoadingState error={error} loaded={loaded}>
      {createdResources.length === 0 ? (
        <EmptyState
          headingLevel="h4"
          titleText="No created resources"
          icon={CubesIcon}
          data-testid="created-resources-empty"
        >
          <EmptyStateBody>
            This workspace has not created any resources in its namespace.
          </EmptyStateBody>
        </EmptyState>
      ) : (
        <Table
          aria-label="Resources created by the workspace"
          variant="compact"
          data-testid="created-resources-table"
        >
          <Thead>
            <Tr>
              <Th>Kind</Th>
              <Th>Name</Th>
              <Th>Created</Th>
              <Th>When workspace is deleted</Th>
            </Tr>
          </Thead>
          <Tbody>
            {createdResources.map((createdResource) => (
              <Tr
                key={`${createdResource.group}/${createdResource.resource}/${createdResource.name}`}
                data-testid={`created-resource-${createdResource.kind}-${createdResource.name}`}
              >
                <Td dataLabel="Kind">
                  {createdResource.kind}
                  <Content component="small">
                    {createdResource.group
                      ? `${createdResource.group}/${createdResource.version}`
                      : createdResource.version}
                  </Content>
                </Td>
                <Td dataLabel="Name">{createdResource.name}</Td>
                <Td dataLabel="Created">
                  {format(new Date(createdResource.audit.createdAt), DATE_FORMAT)}
                </Td>
                <Td dataLabel="When workspace is deleted">
                  {createdResource.deletionPolicy ===
                  V1Beta1WorkspaceKindCreatedResourcesDeletionPolicy.WorkspaceKindCreatedResourcesDeletionPolicyDelete ? (
                    <Label color="orange" isCompact>
                      Deleted
                    </Label>
                  ) : (
                    <Label color="grey" isCompact>
                      Retained
                    </Label>
                  )}
                </Td>
              </Tr>
            ))}
          </Tbody>
        </Table>
      )}
    </DetailsLoadingState>
  );
};
