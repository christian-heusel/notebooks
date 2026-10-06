import { renderHook } from '~/__tests__/unit/testUtils/hooks';
import { useNotebookAPI } from '~/app/hooks/useNotebookAPI';
import { useWorkspaceCreatedResources } from '~/app/hooks/useWorkspaceCreatedResources';
import { NotebookApis } from '~/shared/api/notebookApi';
import { buildMockWorkspaceCreatedResources } from '~/shared/mock/mockBuilder';

jest.mock('~/app/hooks/useNotebookAPI', () => ({
  useNotebookAPI: jest.fn(),
}));

const mockUseNotebookAPI = useNotebookAPI as jest.MockedFunction<typeof useNotebookAPI>;

describe('useWorkspaceCreatedResources', () => {
  beforeEach(() => {
    jest.clearAllMocks();
  });

  it('returns error when API unavailable', async () => {
    mockUseNotebookAPI.mockReturnValue({
      api: {} as NotebookApis,
      apiAvailable: false,
      refreshAllAPI: jest.fn(),
    });
    const { result, waitForNextUpdate } = renderHook(() =>
      useWorkspaceCreatedResources('test-ns', 'test-workspace'),
    );
    await waitForNextUpdate();

    const [createdResources, loaded, error] = result.current;
    expect(createdResources).toEqual([]);
    expect(loaded).toBe(false);
    expect(error).toBeDefined();
  });

  it('stays in initial state when the workspace is not selected', () => {
    mockUseNotebookAPI.mockReturnValue({
      api: {} as NotebookApis,
      apiAvailable: true,
      refreshAllAPI: jest.fn(),
    });
    const { result } = renderHook(() => useWorkspaceCreatedResources('test-ns', undefined));

    const [createdResources, loaded, error] = result.current;
    expect(createdResources).toEqual([]);
    expect(loaded).toBe(false);
    expect(error).toBeUndefined();
  });

  it('fetches the created resources successfully', async () => {
    const mockCreatedResources = buildMockWorkspaceCreatedResources();
    const listWorkspaceCreatedResources = jest
      .fn()
      .mockResolvedValue({ data: mockCreatedResources });
    const api = {
      workspaces: { listWorkspaceCreatedResources },
    } as unknown as NotebookApis;

    mockUseNotebookAPI.mockReturnValue({
      api,
      apiAvailable: true,
      refreshAllAPI: jest.fn(),
    });

    const { result, waitForNextUpdate } = renderHook(() =>
      useWorkspaceCreatedResources('test-ns', 'test-workspace'),
    );
    await waitForNextUpdate();

    const [createdResources, loaded, error] = result.current;
    expect(createdResources).toEqual(mockCreatedResources);
    expect(loaded).toBe(true);
    expect(error).toBeUndefined();
    expect(listWorkspaceCreatedResources).toHaveBeenCalledWith('test-ns', 'test-workspace');
  });

  it('returns an error when the request fails', async () => {
    const listWorkspaceCreatedResources = jest.fn().mockRejectedValue(new Error('boom'));
    const api = {
      workspaces: { listWorkspaceCreatedResources },
    } as unknown as NotebookApis;

    mockUseNotebookAPI.mockReturnValue({
      api,
      apiAvailable: true,
      refreshAllAPI: jest.fn(),
    });

    const { result, waitForNextUpdate } = renderHook(() =>
      useWorkspaceCreatedResources('test-ns', 'test-workspace'),
    );
    await waitForNextUpdate();

    const [createdResources, loaded, error] = result.current;
    expect(createdResources).toEqual([]);
    expect(loaded).toBe(false);
    expect(error?.message).toBe('boom');
  });
});
