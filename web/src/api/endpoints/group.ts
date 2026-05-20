import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { apiClient } from '../client';
import { logger } from '@/lib/logger';

/**
 * 分组项信息
 */
export interface GroupItem {
    id?: number;
    group_id?: number;
    channel_id: number;
    model_name: string;
    priority: number;
    weight: number;
}

/**
 * 分组模式
 */
export enum GroupMode {
    RoundRobin = 1,
    Random = 2,
    Failover = 3,
    Weighted = 4,
}

/**
 * 分组信息
 */
export interface Group {
    id?: number;
    name: string;
    mode: GroupMode;
    match_regex: string;
    first_token_time_out?: number;
    session_keep_time?: number;
    items?: GroupItem[];
}

/**
 * 新增 item 请求
 */
export interface GroupItemAddRequest {
    channel_id: number;
    model_name: string;
    priority: number;
    weight: number;
}

/**
 * 更新 item 请求 (仅 priority)
 */
export interface GroupItemUpdateRequest {
    id: number;
    priority: number;
    weight: number;
}

/**
 * 分组更新请求 - 仅包含变更的数据
 */
export interface GroupUpdateRequest {
    id: number;
    name?: string;                        // 仅在名称变更时发送
    mode?: GroupMode;                     // 仅在模式变更时发送
    match_regex?: string;                 // 仅在匹配正则变更时发送
    first_token_time_out?: number;        // 仅在超时变更时发送
    session_keep_time?: number;           // 仅在会话保持时间变更时发送
    items_to_add?: GroupItemAddRequest[];    // 新增的 items
    items_to_update?: GroupItemUpdateRequest[]; // 更新的 items (priority 变更)
    items_to_delete?: number[];              // 删除的 item IDs
}

/**
 * 获取分组列表 Hook
 * 
 * @example
 * const { data: groups, isLoading, error } = useGroupList();
 * 
 * if (isLoading) return <Loading />;
 * if (error) return <Error message={error.message} />;
 * 
 * groups?.forEach(group => console.log(group.name, group.items));
 */
export function useGroupList() {
    return useQuery({
        queryKey: ['groups', 'list'],
        queryFn: async () => {
            return apiClient.get<Group[]>('/api/v1/group/list');
        },
        refetchInterval: 30000,
        refetchOnMount: 'always',
    });
}

/**
 * 创建分组 Hook
 * 
 * @example
 * const createGroup = useCreateGroup();
 * 
 * createGroup.mutate({
 *   name: 'my-group',
 *   items: [
 *     { channel_id: 1, model_name: 'gpt-4', priority: 1 },
 *   ],
 * });
 */
export function useCreateGroup() {
    const queryClient = useQueryClient();

    return useMutation({
        mutationFn: async (data: Group) => {
            return apiClient.post<Group>('/api/v1/group/create', data);
        },
        onSuccess: (data) => {
            logger.log('分组创建成功:', data);
            queryClient.invalidateQueries({ queryKey: ['groups', 'list'] });
        },
        onError: (error) => {
            logger.error('分组创建失败:', error);
        },
    });
}

/**
 * 更新分组 Hook - 仅发送变更的数据
 * 
 * @example
 * const updateGroup = useUpdateGroup();
 * 
 * updateGroup.mutate({
 *   id: 1,
 *   name: 'updated-group',  // 可选，仅在名称变更时发送
 *   items_to_add: [{ channel_id: 1, model_name: 'gpt-4', priority: 1 }],
 *   items_to_update: [{ id: 1, priority: 2 }],
 *   items_to_delete: [2, 3],
 * });
 */
export function useUpdateGroup() {
    const queryClient = useQueryClient();

    return useMutation({
        mutationFn: async (data: GroupUpdateRequest) => {
            return apiClient.post<Group>('/api/v1/group/update', data);
        },
        onSuccess: (data) => {
            logger.log('分组更新成功:', data);
            // 先用后端返回的新分组同步更新列表缓存，避免编辑弹窗关闭后页面短时间继续显示旧数据。
            if (typeof data.id === 'number') {
                queryClient.setQueryData<Group[]>(['groups', 'list'], (oldGroups) => {
                    if (!oldGroups) return oldGroups;
                    return oldGroups.map((group) => group.id === data.id ? data : group);
                });
            }
            queryClient.invalidateQueries({ queryKey: ['groups', 'list'] });
        },
        onError: (error) => {
            logger.error('分组更新失败:', error);
        },
    });
}

/**
 * 删除分组 Hook
 * 
 * @example
 * const deleteGroup = useDeleteGroup();
 * 
 * deleteGroup.mutate(1); // 删除 ID 为 1 的分组
 */
export function useDeleteGroup() {
    const queryClient = useQueryClient();

    return useMutation({
        mutationFn: async (id: number) => {
            return apiClient.delete<null>(`/api/v1/group/delete/${id}`);
        },
        onSuccess: () => {
            logger.log('分组删除成功');
            queryClient.invalidateQueries({ queryKey: ['groups', 'list'] });
        },
        onError: (error) => {
            logger.error('分组删除失败:', error);
        },
    });
}

/**
 * 自动添加分组 item Hook
*
 * 后端路由: POST /api/v1/group/auto-add-item
 * Body: { id: number }
*
 * @example
 * const autoAdd = useAutoAddGroupItem();
 * autoAdd.mutate(1); // 为 groupId=1 自动添加匹配的 items
 */
// export function useAutoAddGroupItem() {
//     const queryClient = useQueryClient();

//     return useMutation({
//         mutationFn: async (groupId: number) => {
//             return apiClient.post<null>(`/api/v1/group/auto-add-item`, { id: groupId });
//         },
//         onSuccess: () => {
//             logger.log('自动添加分组 item 成功');
//             queryClient.invalidateQueries({ queryKey: ['groups', 'list'] });
//         },
//         onError: (error) => {
//             logger.error('自动添加分组 item 失败:', error);
//         },
//     });
// }

/**
 * 未分组模型信息（用于快速创建分组）
 */
export interface UngroupedModel {
    name: string;
    enabled: boolean;
    channel_id: number;
    channel_name: string;
}

/**
 * 快速创建分组的单个模型配置
 */
export interface QuickGroupItem {
    model_name: string;
    group_name?: string; // 自定义分组名（为空时使用 model_name）
    mode: number;
    match_regex?: string; // 用户自定义正则（优先于自动生成）
}

/**
 * 批量创建分组请求
 */
export interface BatchCreateRequest {
    groups: QuickGroupItem[];
}

/**
 * 批量创建分组的单个结果
 */
export interface BatchCreateResult {
    model_name: string;
    group_id?: number;
    match_count: number;
    error?: string;
}

/**
 * 获取未分组模型列表 Hook
 */
export function useUngroupedModels() {
    return useQuery({
        queryKey: ['groups', 'ungrouped-models'],
        queryFn: async () => {
            return apiClient.get<UngroupedModel[]>('/api/v1/group-template/ungrouped-models');
        },
    });
}

/**
 * 批量创建分组 Hook
 * 缓存失效由调用方在 onSuccess 中自行管理，避免 dialog exit 动画期间触发 refetch 导致卡顿
 */
export function useBatchCreateGroups() {
    return useMutation({
        mutationFn: async (data: BatchCreateRequest) => {
            return apiClient.post<BatchCreateResult[]>('/api/v1/group-template/batch-create', data);
        },
    });
}
