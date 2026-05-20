'use client';

import { memo, useEffect, useId, useRef, useState } from 'react';
import type { CSSProperties, RefObject } from 'react';
import { createPortal } from 'react-dom';
import { Layers, GripVertical, X, Trash2, ArrowUpToLine, ArrowDownToLine } from 'lucide-react';
import {
    DragDropContext,
    Draggable,
    Droppable,
    type DraggableProvided,
    type DropResult,
} from '@hello-pangea/dnd';
import { useVirtualizer } from '@tanstack/react-virtual';
import { motion, AnimatePresence } from 'motion/react';
import { cn } from '@/lib/utils';
import { getModelIcon } from '@/lib/model-icons';
import type { LLMChannel } from '@/api/endpoints/model';
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/animate-ui/components/animate/tooltip';
import { useTranslations } from 'next-intl';

const VIRTUAL_MEMBER_THRESHOLD = 80;
const MEMBER_ROW_ESTIMATE_SIZE = 45;
const dragPortalRoot = typeof document === 'undefined' ? null : document.body;

export interface SelectedMember extends LLMChannel {
    id: string;
    item_id?: number;
    weight?: number;
}

function reorderList<T>(list: T[], startIndex: number, endIndex: number): T[] {
    const result = [...list];
    const [removed] = result.splice(startIndex, 1);
    result.splice(endIndex, 0, removed);
    return result;
}

type MemberItemDnd = {
    innerRef: DraggableProvided['innerRef'];
    draggableProps: DraggableProvided['draggableProps'];
    dragHandleProps: DraggableProvided['dragHandleProps'];
    isDragging: boolean;
};

const memberItemContainStyle: CSSProperties = {
    // 展开分组里模型很多时，浏览器可跳过视口外行的绘制，降低滚动卡顿。
    contentVisibility: 'auto',
    containIntrinsicSize: '44px',
    contain: 'layout paint style',
};

const MemberItem = memo(function MemberItem({
    member,
    onRemove,
    onWeightChange,
    isRemoving,
    index,
    showWeight = false,
    showConfirmDelete = true,
    layoutScope,
    dnd,
    isConfirmingDelete = false,
    onConfirmDeleteChange,
    onMoveToTop,
    onMoveToBottom,
}: {
    member: SelectedMember;
    onRemove: (id: string) => void;
    onWeightChange?: (id: string, weight: number) => void;
    isRemoving?: boolean;
    index: number;
    showWeight?: boolean;
    showConfirmDelete?: boolean;
    layoutScope?: string;
    dnd?: MemberItemDnd;
    isConfirmingDelete?: boolean;
    onConfirmDeleteChange?: (id: string | null) => void;
    onMoveToTop?: (index: number) => void;
    onMoveToBottom?: (index: number) => void;
}) {
    const t = useTranslations('group');
    const { Avatar: ModelAvatar } = getModelIcon(member.name);
    const isDisabled = member.enabled === false;

    return (
        <div
            ref={dnd?.innerRef}
            {...(dnd?.draggableProps ?? {})}
            className={cn('rounded-lg grid transition-[grid-template-rows] duration-200', isRemoving ? 'grid-rows-[0fr]' : 'grid-rows-[1fr]')}
            style={{
                ...(dnd?.draggableProps?.style ?? {}),
                ...(dnd ? null : memberItemContainStyle),
                ...(dnd?.isDragging ? { zIndex: 50, boxShadow: '0 8px 32px rgba(0,0,0,0.15)' } : null),
            }}
        >
            <div className={cn(
                'flex items-center gap-2 rounded-lg bg-background border border-border/50 px-2.5 py-2 select-none transition-opacity duration-200 relative overflow-hidden',
                isRemoving && 'opacity-0',
                isDisabled && 'opacity-60 grayscale'
            )}>
                <span className={cn(
                    'size-5 rounded-md text-xs font-bold grid place-items-center shrink-0',
                    isDisabled ? 'bg-muted text-muted-foreground' : 'bg-primary/10 text-primary'
                )}>
                    {index + 1}
                </span>

                <div
                    className={cn(
                        'p-0.5 rounded touch-none transition-colors',
                        dnd
                            ? (isDisabled
                                ? 'cursor-grab active:cursor-grabbing hover:bg-muted/60'
                                : 'cursor-grab active:cursor-grabbing hover:bg-muted')
                            : 'cursor-default text-muted-foreground/50'
                    )}
                    {...(dnd?.dragHandleProps ?? {})}
                >
                    <GripVertical className="size-3.5 text-muted-foreground" />
                </div>

                <span className={cn(isDisabled && 'opacity-70')}>
                    <ModelAvatar size={18} />
                </span>

                <div className="flex flex-col min-w-0 flex-1">
                    <Tooltip side="top" sideOffset={10} align="start">
                        <TooltipTrigger className={cn(
                            'text-sm font-medium truncate leading-tight',
                            isDisabled && 'text-muted-foreground'
                        )}>
                            {member.name}
                        </TooltipTrigger>
                        <TooltipContent key={member.name}>{member.name}</TooltipContent>
                    </Tooltip>
                    <span className="text-[10px] text-muted-foreground truncate leading-tight">{member.channel_name}</span>
                </div>

                {showWeight && (
                    <input
                        type="number"
                        min={1}
                        value={member.weight ?? 1}
                        onChange={(e) => onWeightChange?.(member.id, Math.max(1, parseInt(e.target.value) || 1))}
                        className={cn(
                            'w-12 h-6 text-xs text-center rounded border border-border bg-muted/50 focus:outline-none focus:ring-1 focus:ring-primary',
                            isDisabled && 'text-muted-foreground'
                        )}
                    />
                )}

                {onMoveToTop && (
                    <button
                        type="button"
                        onClick={() => onMoveToTop(index)}
                        className="p-1 rounded hover:bg-muted text-muted-foreground hover:text-foreground transition-colors"
                        title={t('form.moveToTop')}
                    >
                        <ArrowUpToLine className="size-3" />
                    </button>
                )}
                {onMoveToBottom && (
                    <button
                        type="button"
                        onClick={() => onMoveToBottom(index)}
                        className="p-1 rounded hover:bg-muted text-muted-foreground hover:text-foreground transition-colors"
                        title={t('form.moveToBottom')}
                    >
                        <ArrowDownToLine className="size-3" />
                    </button>
                )}

                {(!showConfirmDelete || !isConfirmingDelete) && (
                    <motion.button
                        layoutId={`delete-btn-member-${layoutScope ?? 'default'}-${member.id}`}
                        type="button"
                        onClick={() => showConfirmDelete ? onConfirmDeleteChange?.(member.id) : onRemove(member.id)}
                        className="p-1 rounded hover:bg-destructive/10 hover:text-destructive transition-colors"
                        initial={false}
                        animate={{ opacity: 1, x: 0 }}
                        transition={{ duration: 0.15 }}
                        style={{ pointerEvents: 'auto' }}
                    >
                        <X className="size-3" />
                    </motion.button>
                )}

                <AnimatePresence>
                    {showConfirmDelete && isConfirmingDelete && (
                        <motion.div
                            layoutId={`delete-btn-member-${layoutScope ?? 'default'}-${member.id}`}
                            className="absolute inset-0 flex items-center justify-center gap-2 bg-destructive p-1.5 rounded-lg"
                            transition={{ type: 'spring', stiffness: 400, damping: 30 }}
                        >
                            <button
                                type="button"
                                onClick={() => onConfirmDeleteChange?.(null)}
                                className="flex h-6 w-6 items-center justify-center rounded-md bg-destructive-foreground/20 text-destructive-foreground transition-all hover:bg-destructive-foreground/30 active:scale-95"
                            >
                                <X className="h-3 w-3" />
                            </button>
                            <button
                                type="button"
                                onClick={() => {
                                    onConfirmDeleteChange?.(null);
                                    onRemove(member.id);
                                }}
                                className="flex-1 h-6 flex items-center justify-center gap-1.5 rounded-md bg-destructive-foreground text-destructive text-xs font-semibold transition-all hover:bg-destructive-foreground/90 active:scale-[0.98]"
                            >
                                <Trash2 className="h-3 w-3" />
                            </button>
                        </motion.div>
                    )}
                </AnimatePresence>
            </div>
        </div>
    );
});

export interface MemberListProps {
    members: SelectedMember[];
    onReorder: (members: SelectedMember[]) => void;
    onRemove: (id: string) => void;
    onWeightChange?: (id: string, weight: number) => void;
    /**
     * When true, auto-scroll the list to bottom when a *new visible* member appears
     * (i.e. a new member id is added). Useful in "editor" flows. Defaults to true.
     */
    autoScrollOnAdd?: boolean;
    onDragStart?: () => void;
    /**
     * Called only when a drop results in a different order (i.e. commit reorder).
     * Useful for persisting the new order.
     */
    onDrop?: (members: SelectedMember[]) => void;
    /**
     * Called whenever a drag ends (including cancel / same-index drop).
     * Useful for lifecycle cleanup (e.g. clearing "isDragging" flags).
     */
    onDragFinish?: () => void;
    removingIds?: Set<string>;
    showWeight?: boolean;
    /**
     * When true, show a confirmation overlay before removing an item.
     * When false, clicking the delete button removes the item immediately.
     * Defaults to true.
     */
    showConfirmDelete?: boolean;
    layoutScope?: string;
    /**
     * 卡片展开预览专用：大列表启用虚拟滚动，避免一次性挂载所有 DnD 节点。
     * 编辑弹窗默认关闭，保留完整拖拽排序能力。
     */
    virtualizeLargeList?: boolean;
}

interface VirtualMemberListProps {
    members: SelectedMember[];
    removingIds: Set<string>;
    scrollContainerRef: RefObject<HTMLDivElement | null>;
    layoutScope: string;
    showWeight: boolean;
    showConfirmDelete: boolean;
    confirmDeleteId: string | null;
    onConfirmDeleteChange: (id: string | null) => void;
    onRemove: (id: string) => void;
    onWeightChange?: (id: string, weight: number) => void;
    onMoveToTop: (index: number) => void;
    onMoveToBottom: (index: number) => void;
}

function VirtualMemberList({
    members,
    removingIds,
    scrollContainerRef,
    layoutScope,
    showWeight,
    showConfirmDelete,
    confirmDeleteId,
    onConfirmDeleteChange,
    onRemove,
    onWeightChange,
    onMoveToTop,
    onMoveToBottom,
}: VirtualMemberListProps) {
    'use no memo';

    // 只有大列表性能模式会挂载虚拟滚动，避免小列表也初始化虚拟器造成额外开销。
    // eslint-disable-next-line react-hooks/incompatible-library
    const virtualizer = useVirtualizer({
        count: members.length,
        getScrollElement: () => scrollContainerRef.current,
        getItemKey: (index) => members[index]?.id ?? `member-${index}`,
        estimateSize: () => MEMBER_ROW_ESTIMATE_SIZE,
        overscan: 8,
    });

    return (
        <div className="relative p-2" style={{ height: `${virtualizer.getTotalSize()}px` }}>
            {virtualizer.getVirtualItems().map((virtualRow) => {
                const member = members[virtualRow.index];
                if (!member) return null;

                return (
                    <div
                        key={virtualRow.key}
                        data-index={virtualRow.index}
                        ref={virtualizer.measureElement}
                        className="absolute left-0 top-0 w-full px-2"
                        style={{ transform: `translateY(${virtualRow.start}px)` }}
                    >
                        <MemberItem
                            member={member}
                            onRemove={onRemove}
                            onWeightChange={onWeightChange}
                            isRemoving={removingIds.has(member.id)}
                            index={virtualRow.index}
                            showWeight={showWeight}
                            showConfirmDelete={showConfirmDelete}
                            layoutScope={layoutScope}
                            isConfirmingDelete={confirmDeleteId === member.id}
                            onConfirmDeleteChange={onConfirmDeleteChange}
                            onMoveToTop={onMoveToTop}
                            onMoveToBottom={onMoveToBottom}
                        />
                    </div>
                );
            })}
        </div>
    );
}

export function MemberList({
    members,
    onReorder,
    onRemove,
    onWeightChange,
    autoScrollOnAdd = true,
    onDragStart,
    onDrop,
    onDragFinish,
    removingIds = new Set(),
    showWeight = false,
    showConfirmDelete = true,
    layoutScope: externalLayoutScope,
    virtualizeLargeList = false,
}: MemberListProps) {
    const internalLayoutScope = useId();
    const layoutScope = externalLayoutScope ?? internalLayoutScope;

    const scrollContainerRef = useRef<HTMLDivElement | null>(null);
    const prevMemberCountRef = useRef<number>(0);
    const hasMountedRef = useRef(false);
    const [confirmDeleteId, setConfirmDeleteId] = useState<string | null>(null);

    const visibleCount = members.filter((m) => !removingIds.has(m.id)).length;
    const isEmpty = visibleCount === 0;
    const useVirtualMembers = virtualizeLargeList && members.length > VIRTUAL_MEMBER_THRESHOLD;
    const t = useTranslations('group');

    useEffect(() => {
        // Skip the initial mount so we don't auto-scroll on first render / initial data load.
        if (!hasMountedRef.current) {
            hasMountedRef.current = true;
            prevMemberCountRef.current = members.length;
            return;
        }

        if (!autoScrollOnAdd) {
            prevMemberCountRef.current = members.length;
            return;
        }

        const hasNewMember = members.length > prevMemberCountRef.current;

        // Auto-scroll only when member count increases (i.e. added; not reorder / not "unhide").
        if (hasNewMember) {
            // Wait a tick for DOM/placeholder/layout to settle.
            requestAnimationFrame(() => {
                const el = scrollContainerRef.current;
                if (!el) return;
                el.scrollTo({ top: el.scrollHeight, behavior: 'smooth' });
            });
        }

        prevMemberCountRef.current = members.length;
    }, [members.length, autoScrollOnAdd]);

    const handleDragEnd = (result: DropResult) => {
        try {
            const { destination, source } = result;
            if (!destination) return;
            if (destination.index === source.index) return;

            const next = reorderList(members, source.index, destination.index);
            onReorder(next);
            onDrop?.(next);
        } finally {
            // Ensure drag lifecycle always finishes, even when drop is canceled.
            onDragFinish?.();
        }
    };

    const handleMoveToTop = (index: number) => {
        if (index === 0) return;
        const next = reorderList(members, index, 0);
        onReorder(next);
        onDrop?.(next);
    };

    const handleMoveToBottom = (index: number) => {
        if (index === members.length - 1) return;
        const next = reorderList(members, index, members.length - 1);
        onReorder(next);
        onDrop?.(next);
    };

    return (
        <div className="relative flex h-full min-h-0 flex-col">
            <div
                className={cn(
                    'absolute inset-0 flex flex-col items-center justify-center gap-2 text-muted-foreground',
                    'transition-opacity duration-200 ease-out',
                    isEmpty ? 'opacity-100' : 'opacity-0 pointer-events-none'
                )}
            >
                <Layers className="size-10 opacity-40" />
                <span className="text-sm">{t('card.empty')}</span>
            </div>

            {useVirtualMembers && !isEmpty && (
                <div className="shrink-0 border-b border-border/40 bg-muted/40 px-3 py-2 text-[11px] leading-4 text-muted-foreground">
                    {t('card.performanceMode')}
                </div>
            )}

            <div
                className={cn(
                    'min-h-0 flex-1 overflow-y-auto transition-opacity duration-200',
                    isEmpty ? 'opacity-0' : 'opacity-100'
                )}
                ref={scrollContainerRef}
            >
                {useVirtualMembers ? (
                    <VirtualMemberList
                        members={members}
                        removingIds={removingIds}
                        scrollContainerRef={scrollContainerRef}
                        layoutScope={layoutScope}
                        showWeight={showWeight}
                        showConfirmDelete={showConfirmDelete}
                        confirmDeleteId={confirmDeleteId}
                        onConfirmDeleteChange={setConfirmDeleteId}
                        onRemove={onRemove}
                        onWeightChange={onWeightChange}
                        onMoveToTop={handleMoveToTop}
                        onMoveToBottom={handleMoveToBottom}
                    />
                ) : (
                    <DragDropContext
                        onDragStart={() => {
                            onDragStart?.();
                        }}
                        onDragEnd={handleDragEnd}
                    >
                        <Droppable droppableId={`members-${layoutScope}`}>
                            {(droppableProvided) => (
                                <div
                                    ref={droppableProvided.innerRef}
                                    {...droppableProvided.droppableProps}
                                    className="p-2 flex flex-col space-y-1.5"
                                >
                                    {members.map((member, index) => (
                                        <Draggable
                                            key={member.id}
                                            draggableId={member.id}
                                            index={index}
                                            isDragDisabled={removingIds.has(member.id)}
                                        >
                                            {(draggableProvided, snapshot) => {
                                                const item = (
                                                    <MemberItem
                                                        member={member}
                                                        onRemove={onRemove}
                                                        onWeightChange={onWeightChange}
                                                        isRemoving={removingIds.has(member.id)}
                                                        index={index}
                                                        showWeight={showWeight}
                                                        showConfirmDelete={showConfirmDelete}
                                                        layoutScope={layoutScope}
                                                        isConfirmingDelete={confirmDeleteId === member.id}
                                                        onConfirmDeleteChange={setConfirmDeleteId}
                                                        dnd={{
                                                            innerRef: draggableProvided.innerRef,
                                                            draggableProps: draggableProvided.draggableProps,
                                                            dragHandleProps: draggableProvided.dragHandleProps,
                                                            isDragging: snapshot.isDragging,
                                                        }}
                                                        onMoveToTop={handleMoveToTop}
                                                        onMoveToBottom={handleMoveToBottom}
                                                    />
                                                );

                                                // 拖拽库使用 fixed 坐标；放到 body 下可避开上层 motion transform / 弹窗坐标系导致的拖拽偏移。
                                                if (snapshot.isDragging && dragPortalRoot) {
                                                    return createPortal(item, dragPortalRoot);
                                                }

                                                return item;
                                            }}
                                        </Draggable>
                                    ))}
                                    {droppableProvided.placeholder}
                                </div>
                            )}
                        </Droppable>
                    </DragDropContext>
                )}
            </div>
        </div>
    );
}
