import { useCallback } from 'react';
import { FetchState, FetchStateCallbackPromise, useFetchState, NotReadyError } from 'mod-arch-core';
import { useNotebookAPI } from '~/app/hooks/useNotebookAPI';
import { CreatedresourcesCreatedResource } from '~/generated/data-contracts';

export const useWorkspaceCreatedResources = (
  namespace: string | undefined,
  name: string | undefined,
): FetchState<CreatedresourcesCreatedResource[]> => {
  const { api, apiAvailable } = useNotebookAPI();

  const call = useCallback<
    FetchStateCallbackPromise<CreatedresourcesCreatedResource[]>
  >(async () => {
    if (!apiAvailable) {
      return Promise.reject(new Error('API not yet available'));
    }
    if (!namespace || !name) {
      return Promise.reject(new NotReadyError('Workspace not yet selected'));
    }
    const response = await api.workspaces.listWorkspaceCreatedResources(namespace, name);
    return response.data;
  }, [api.workspaces, apiAvailable, namespace, name]);

  return useFetchState(call, []);
};
