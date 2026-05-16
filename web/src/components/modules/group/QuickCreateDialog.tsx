'use client';

import { memo, useCallback, useMemo, useRef, useState, useTransition } from 'react';
import { Pencil, Search, Zap, X } from 'lucide-react';
import { Switch } from '@/components/ui/switch';
import { useTranslations } from 'next-intl';
import {
    MorphingDialogClose,
    MorphingDialogTitle,
    MorphingDialogDescription,
    useMorphingDialog,
} from '@/components/ui/morphing-dialog';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import {
    Select,
    SelectContent,
    SelectItem,
    SelectTrigger,
    SelectValue,
} from '@/components/ui/select';
import { cn } from '@/lib/utils';
import { toast } from '@/components/common/Toast';
import {
    useUngroupedModels,
    useBatchCreateGroups,
    type UngroupedModel,
    type QuickGroupItem,
} from '@/api/endpoints/group';
import { GroupMode } from '@/api/endpoints/group';
import { useQueryClient } from '@tanstack/react-query';
import { useVirtualizer } from '@tanstack/react-virtual';

/** 每个选中模型的独立配置 */
interface ModelConfig {
    mode: GroupMode;
    exclude: string[];
    matchRegex: string; // 用户自定义正则，为空则自动生成
}

/** 模型名解析结果：找到第一个分隔符，前面为前缀，后面为后缀 */
function parseModelName(name: string): { prefix: string; suffix: string } {
    const match = name.match(/[-_.]/);
    if (!match || match.index === undefined) return { prefix: name, suffix: '' };
    return { prefix: name.slice(0, match.index), suffix: name.slice(match.index + 1) };
}

function escapeRegex(str: string): string {
    return str.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
}

/** 将后缀中的版本号分隔符替换为字符类 [._-] */
function cleanSuffix(suffix: string): string {
    const re = /(\d+)[._-](\d+)/;
    let result = suffix;
    let prev: string;
    do {
        prev = result;
        result = result.replace(re, '$1[._-]$2');
    } while (result !== prev);
    return result;
}

/** 根据模型名和排除字段生成正则字符串（用于提交） */
function generateAutoRegex(name: string, exclude: string[]): string {
    const { prefix, suffix } = parseModelName(name);
    let regex = '(?i)';

    if (exclude.length > 0) {
        regex += `^(?!.*(${exclude.map(escapeRegex).join('|')}))`;
    } else {
        regex += '^';
    }

    regex += `.*${escapeRegex(prefix)}`;
    if (suffix) {
        regex += `.*${cleanSuffix(suffix)}`;
    }
    regex += '.*$';
    return regex;
}

/** 根据模型名和排除字段生成高亮的正则 React 元素（替代 dangerouslySetInnerHTML） */
function buildRegexDisplay(name: string, exclude: string[]): { element: React.ReactNode; regex: string } {
    const { prefix, suffix } = parseModelName(name);
    let regex = '(?i)';

    const parts: React.ReactNode[] = [
        <span key="flag" className="text-yellow-400">(?i)</span>,
    ];

    if (exclude.length > 0) {
        const exStr = exclude.map(escapeRegex).join('|');
        regex += `^(?!.*(${exStr}))`;
        // 显示时去掉正则转义符号，仅展示用户输入的原始排除词
        parts.push(
            <span key="neg">
                {'^(?!.*('}
                <span className="text-red-400">{exclude.join('|')}</span>
                {'))'}
            </span>,
        );
    } else {
        regex += '^';
        parts.push(<span key="start">^</span>);
    }

    regex += `.*${escapeRegex(prefix)}`;
    parts.push(
        <span key="prefix">
            .*<span className="text-emerald-400">{prefix}</span>
        </span>,
    );

    if (suffix) {
        const cleaned = cleanSuffix(suffix);
        regex += `.*${cleaned}`;
        parts.push(
            <span key="suffix">
                .*<span className="text-indigo-400">{cleaned}</span>
            </span>,
        );
    }

    regex += '.*$';
    parts.push(<span key="end">.*$</span>);

    return { element: parts, regex };
}

const MODE_OPTIONS = [
    { value: GroupMode.RoundRobin, labelKey: 'mode.roundRobin' },
    { value: GroupMode.Random, labelKey: 'mode.random' },
    { value: GroupMode.Failover, labelKey: 'mode.failover' },
    { value: GroupMode.Weighted, labelKey: 'mode.weighted' },
];

// 虚拟滚动阈值：超过此数量启用虚拟滚动
const VIRTUAL_THRESHOLD = 50;

/** 模型列表项（memo 优化，配合虚拟滚动减少重渲染） */
const ModelItem = memo(function ModelItem({
    model,
    selected,
    onToggle,
}: {
    model: UngroupedModel;
    selected: boolean;
    onToggle: () => void;
}) {
    const t = useTranslations('group.quickCreate');

    return (
        <button
            type="button"
            onClick={onToggle}
            className={cn(
                'w-full flex items-center gap-2.5 px-3 py-2 text-left transition-colors border-b border-border/30 last:border-0',
                selected ? 'bg-primary/10' : 'hover:bg-muted/50'
            )}
        >
            <input
                type="checkbox"
                checked={selected}
                onChange={onToggle}
                onClick={(e) => e.stopPropagation()}
                className="accent-primary w-4 h-4 cursor-pointer shrink-0"
            />
            <span className="text-sm font-medium flex-1 min-w-0 truncate">{model.name}</span>
            <span className="text-xs text-muted-foreground bg-muted px-2 py-0.5 rounded-md max-w-[100px] truncate shrink-0" title={model.channel_name}>
                {model.channel_name}
            </span>
            {!model.enabled && (
                <span className="text-xs text-muted-foreground bg-muted px-1.5 py-0.5 rounded shrink-0">
                    {t('disabled')}
                </span>
            )}
        </button>
    );
});

/** 单个模型的配置卡片（React.memo 防止兄弟卡片变化导致的无效重渲染） */
const ModelConfigCard = memo(function ModelConfigCard({
    name,
    config,
    useAutoRegex,
    onModeChange,
    onAddExclude,
    onRemoveExclude,
    onRemove,
    onRegexChange,
}: {
    name: string;
    config: ModelConfig;
    useAutoRegex: boolean;
    onModeChange: (mode: GroupMode) => void;
    onAddExclude: (term: string) => void;
    onRemoveExclude: (index: number) => void;
    onRemove: () => void;
    onRegexChange: (regex: string) => void;
}) {
    const t = useTranslations('group');
    const tq = useTranslations('group.quickCreate');
    const [excludeInput, setExcludeInput] = useState('');
    const [editingRegex, setEditingRegex] = useState(false);
    const { prefix, suffix } = parseModelName(name);

    // useMemo 缓存正则显示结果，仅在 name 或 exclude 变化时重新计算
    const { element: regexElement, regex: autoRegex } = useMemo(
        () => buildRegexDisplay(name, config.exclude),
        [name, config.exclude]
    );

    const handleAddExclude = () => {
        const val = excludeInput.trim();
        if (val && !config.exclude.includes(val)) {
            onAddExclude(val);
            setExcludeInput('');
        }
    };

    return (
        <div className="border border-border/50 rounded-xl p-3 bg-muted/20 hover:border-border/80 transition-colors">
            {/* 头部：模型名 + 删除 */}
            <div className="flex items-center gap-2 mb-2.5">
                <span className="text-sm font-semibold text-blue-400 flex-1">{name}</span>
                <button
                    type="button"
                    onClick={onRemove}
                    className="text-muted-foreground hover:text-red-400 hover:bg-red-950/40 rounded p-0.5 transition-colors"
                    title="移除"
                >
                    <X className="size-4" />
                </button>
            </div>

            {/* 配置行：模式 + 排除 */}
            <div className="flex gap-3 items-start mb-2.5">
                {/* 模式选择 */}
                <div className="shrink-0">
                    <label className="text-xs text-muted-foreground block mb-1">{tq('mode')}</label>
                    <Select value={String(config.mode)} onValueChange={(v) => onModeChange(Number(v) as GroupMode)}>
                        <SelectTrigger className="h-8 w-[100px] text-xs rounded-lg">
                            <SelectValue />
                        </SelectTrigger>
                        <SelectContent>
                            {MODE_OPTIONS.map((opt) => (
                                <SelectItem key={opt.value} value={String(opt.value)} className="text-xs">
                                    {t(opt.labelKey)}
                                </SelectItem>
                            ))}
                        </SelectContent>
                    </Select>
                </div>

                {/* 排除字段 */}
                <div className="flex-1 min-w-0">
                    <label className="text-xs text-muted-foreground block mb-1">{tq('exclude')}</label>
                    <div className="flex gap-1.5 items-center flex-wrap">
                        <Input
                            value={excludeInput}
                            onChange={(e) => setExcludeInput(e.target.value)}
                            onKeyDown={(e) => e.key === 'Enter' && (e.preventDefault(), handleAddExclude())}
                            placeholder={tq('excludePlaceholder')}
                            className="h-8 w-[100px] text-xs rounded-lg"
                        />
                        <Button
                            type="button"
                            variant="outline"
                            size="sm"
                            className="h-8 text-xs rounded-lg px-2"
                            onClick={handleAddExclude}
                        >
                            {tq('excludeAdd')}
                        </Button>
                        {config.exclude.map((term, i) => (
                            <span
                                key={i}
                                className="inline-flex items-center gap-1 bg-red-950/50 border border-red-800/50 text-red-300 px-2 py-0.5 rounded-md text-xs"
                            >
                                {term}
                                <button
                                    type="button"
                                    onClick={() => onRemoveExclude(i)}
                                    className="opacity-70 hover:opacity-100"
                                >
                                    <X className="size-3" />
                                </button>
                            </span>
                        ))}
                    </div>
                </div>
            </div>

            {/* 正则区域 */}
            {useAutoRegex ? (
                <>
                    <div className="relative group/regex">
                        {editingRegex ? (
                            <Input
                                value={config.matchRegex || autoRegex}
                                onChange={(e) => onRegexChange(e.target.value)}
                                onBlur={(e) => {
                                    if (e.target.value === autoRegex) onRegexChange('');
                                    setEditingRegex(false);
                                }}
                                onKeyDown={(e) => e.key === 'Enter' && (e.target as HTMLInputElement).blur()}
                                autoFocus
                                className="font-mono text-xs h-auto py-2 rounded-lg"
                            />
                        ) : (
                            <div
                                className="font-mono text-xs text-muted-foreground bg-background/50 px-2.5 py-2 rounded-lg break-all leading-relaxed cursor-text"
                                onClick={() => { onRegexChange(''); setEditingRegex(true); }}
                            >
                                {/* 使用 React 元素替代 dangerouslySetInnerHTML，避免 XSS 风险 */}
                                {config.matchRegex ? config.matchRegex : regexElement}
                            </div>
                        )}
                        {!editingRegex && (
                            <button
                                type="button"
                                onClick={() => { onRegexChange(''); setEditingRegex(true); }}
                                className="absolute right-1.5 top-1.5 opacity-0 group-hover/regex:opacity-100 text-muted-foreground hover:text-foreground transition-opacity"
                                title="编辑正则"
                            >
                                <Pencil className="size-3" />
                            </button>
                        )}
                    </div>
                    <div className="text-[11px] text-muted-foreground/60 mt-1 flex items-center gap-2">
                        <span>{tq('prefix')}: {prefix}{suffix ? ` | ${tq('suffix')}: ${suffix}` : ''}</span>
                        {config.matchRegex && (
                            <button
                                type="button"
                                onClick={() => onRegexChange('')}
                                className="text-blue-400 hover:text-blue-300 text-[11px]"
                            >
                                恢复自动
                            </button>
                        )}
                    </div>
                </>
            ) : (
                <Input
                    value={config.matchRegex}
                    onChange={(e) => onRegexChange(e.target.value)}
                    placeholder={tq('manualRegexPlaceholder')}
                    className="font-mono text-xs h-auto py-2 rounded-lg"
                />
            )}
        </div>
    );
});

/** 快速创建分组对话框主体 */
export function QuickCreateDialogContent() {
    const { setIsOpen } = useMorphingDialog();
    const t = useTranslations('group');
    const tq = useTranslations('group.quickCreate');
    const queryClient = useQueryClient();
    const [, startTransition] = useTransition();

    const { data: ungroupedModels = [], isLoading } = useUngroupedModels();
    const batchCreate = useBatchCreateGroups();

    const [search, setSearch] = useState('');
    const [useAutoRegex, setUseAutoRegex] = useState(true);
    const [modelConfigs, setModelConfigs] = useState<Record<string, ModelConfig>>({});
    // 提交中标记：防止 exit 动画期间按钮从"创建中..."闪烁回正常状态导致布局抖动
    const [submitted, setSubmitted] = useState(false);

    // 左侧模型列表的滚动容器引用（虚拟滚动需要）
    const listScrollRef = useRef<HTMLDivElement>(null);

    const selectedNames = useMemo(() => Object.keys(modelConfigs), [modelConfigs]);

    const filteredModels = useMemo(() => {
        const term = search.toLowerCase().trim();
        if (!term) return ungroupedModels;
        return ungroupedModels.filter((m) => m.name.toLowerCase().includes(term));
    }, [ungroupedModels, search]);

    const allFilteredSelected = useMemo(
        () => filteredModels.length > 0 && filteredModels.every((m) => m.name in modelConfigs),
        [filteredModels, modelConfigs]
    );

    // 虚拟滚动：仅当模型数量超过阈值时启用
    const useVirtualList = filteredModels.length > VIRTUAL_THRESHOLD;

    const virtualizer = useVirtualizer({
        count: filteredModels.length,
        getScrollElement: () => listScrollRef.current,
        estimateSize: () => 42, // ModelItem 预估行高
        overscan: 5,
    });

    const toggleModel = useCallback((name: string) => {
        setModelConfigs((prev) => {
            const next = { ...prev };
            if (next[name]) {
                delete next[name];
            } else {
                next[name] = { mode: GroupMode.RoundRobin, exclude: [], matchRegex: '' };
            }
            return next;
        });
    }, []);

    const toggleSelectAll = useCallback(() => {
        setModelConfigs((prev) => {
            const next = { ...prev };
            if (allFilteredSelected) {
                filteredModels.forEach((m) => delete next[m.name]);
            } else {
                filteredModels.forEach((m) => {
                    if (!next[m.name]) {
                        next[m.name] = { mode: GroupMode.RoundRobin, exclude: [], matchRegex: '' };
                    }
                });
            }
            return next;
        });
    }, [filteredModels, allFilteredSelected]);

    const setMode = useCallback((name: string, mode: GroupMode) => {
        setModelConfigs((prev) => ({
            ...prev,
            [name]: { ...prev[name], mode },
        }));
    }, []);

    const addExclude = useCallback((name: string, term: string) => {
        setModelConfigs((prev) => {
            const cfg = prev[name];
            if (!cfg || cfg.exclude.includes(term)) return prev;
            const newExclude = [...cfg.exclude, term];

            let matchRegex = cfg.matchRegex;
            // 手动模式下，如果没有自定义正则，生成简单的正则
            if (!useAutoRegex && !matchRegex) {
                const excludePart = newExclude.length > 0 ? `^(?!.*(${newExclude.map(escapeRegex).join('|')}))` : '^';
                matchRegex = `(?i)${excludePart}.*${escapeRegex(name)}.*$`;
            }

            return { ...prev, [name]: { ...cfg, exclude: newExclude, matchRegex } };
        });
    }, [useAutoRegex]);

    const removeExclude = useCallback((name: string, index: number) => {
        setModelConfigs((prev) => {
            const cfg = prev[name];
            if (!cfg) return prev;
            const newExclude = cfg.exclude.filter((_, i) => i !== index);

            // 如果是自动生成的正则，删除排除词后重新生成
            let matchRegex = cfg.matchRegex;
            if (!useAutoRegex && matchRegex) {
                const excludePart = cfg.exclude.length > 0 ? `^(?!.*(${cfg.exclude.map(escapeRegex).join('|')}))` : '^';
                const currentAutoRegex = `(?i)${excludePart}.*${escapeRegex(name)}.*$`;
                if (matchRegex === currentAutoRegex) {
                    if (newExclude.length > 0) {
                        const newExcludePart = `^(?!.*(${newExclude.map(escapeRegex).join('|')}))`;
                        matchRegex = `(?i)${newExcludePart}.*${escapeRegex(name)}.*$`;
                    } else {
                        matchRegex = '';
                    }
                }
            }

            return { ...prev, [name]: { ...cfg, exclude: newExclude, matchRegex } };
        });
    }, [useAutoRegex]);

    const removeModel = useCallback((name: string) => {
        setModelConfigs((prev) => {
            const next = { ...prev };
            delete next[name];
            return next;
        });
    }, []);

    const setMatchRegex = useCallback((name: string, matchRegex: string) => {
        setModelConfigs((prev) => ({
            ...prev,
            [name]: { ...prev[name], matchRegex },
        }));
    }, []);

    const handleBatchCreate = useCallback(() => {
        const groups: QuickGroupItem[] = selectedNames.map((name) => {
            const cfg = modelConfigs[name];
            // 自动正则开启时，无自定义正则则提交自动生成的；关闭时仅提交用户手填的
            const regex = cfg.matchRegex || (useAutoRegex ? generateAutoRegex(name, cfg.exclude) : '');
            return {
                model_name: name,
                mode: cfg.mode,
                match_regex: regex || undefined,
            };
        });

        setSubmitted(true);
        batchCreate.mutate(
            { groups },
            {
                onSuccess: (results) => {
                    const successCount = results.filter((r) => !r.error).length;
                    const failCount = results.filter((r) => r.error).length;
                    if (failCount > 0) {
                        toast.warning(tq('toast.createSuccess', { count: successCount }), {
                            description: `${failCount} 个分组创建失败`,
                        });
                    } else {
                        toast.success(tq('toast.createSuccess', { count: successCount }));
                    }
                    // 使用 startTransition 延迟关闭，让 exit 动画在按钮状态稳定后执行
                    startTransition(() => {
                        setIsOpen(false);
                    });
                    // 在关闭动画结束后刷新缓存（使用 requestAnimationFrame 确保在下一帧执行）
                    requestAnimationFrame(() => {
                        queryClient.invalidateQueries({ queryKey: ['groups', 'list'] });
                        queryClient.invalidateQueries({ queryKey: ['groups', 'ungrouped-models'] });
                    });
                },
                onError: (error) => {
                    setSubmitted(false);
                    toast.error(tq('toast.createFailed'), { description: error.message });
                },
            }
        );
    }, [selectedNames, modelConfigs, useAutoRegex, batchCreate, setIsOpen, tq, queryClient, startTransition]);

    return (
        <div className="w-screen max-w-full md:max-w-3xl h-[calc(100vh-2rem)] min-h-0 flex flex-col">
            <MorphingDialogTitle className="shrink-0">
                <header className="mb-4 flex items-center justify-between">
                    <h2 className="text-2xl font-bold text-card-foreground flex items-center gap-2">
                        <Zap className="size-6 text-yellow-400" />
                        {tq('title')}
                    </h2>
                    <MorphingDialogClose
                        className="relative right-0 top-0"
                        variants={{
                            initial: { opacity: 0, scale: 0.8 },
                            animate: { opacity: 1, scale: 1 },
                            exit: { opacity: 0, scale: 0.8 },
                        }}
                    />
                </header>
            </MorphingDialogTitle>

            <MorphingDialogDescription className="flex-1 min-h-0 overflow-hidden flex flex-col gap-3">
                {/* 搜索栏 */}
                <div className="flex gap-2 items-center shrink-0">
                    <div className="relative flex-1">
                        <Search className="pointer-events-none absolute left-3 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
                        <Input
                            value={search}
                            onChange={(e) => setSearch(e.target.value)}
                            placeholder={tq('searchPlaceholder')}
                            className="h-9 pl-9 rounded-xl"
                        />
                    </div>
                    <Button
                        type="button"
                        variant="outline"
                        size="sm"
                        className="h-9 rounded-xl text-xs shrink-0"
                        onClick={toggleSelectAll}
                        disabled={filteredModels.length === 0}
                    >
                        {tq('selectAll')}
                    </Button>
                    <div className="flex items-center gap-1.5 shrink-0">
                        <Switch
                            checked={useAutoRegex}
                            onCheckedChange={setUseAutoRegex}
                            className="h-4 w-7"
                        />
                        <span className="text-xs text-muted-foreground whitespace-nowrap">{tq('autoRegex')}</span>
                    </div>
                </div>

                {/* 主内容区：左右布局 */}
                <div className="flex-1 min-h-0 grid grid-cols-1 md:grid-cols-2 gap-3 overflow-hidden">
                    {/* 左侧：待选模型列表（支持虚拟滚动） */}
                    <div className="flex flex-col min-h-0 rounded-xl border border-border/50 bg-muted/30 overflow-hidden">
                        <div className="px-3 py-2 border-b border-border/30 bg-muted/50 shrink-0">
                            <span className="text-sm font-medium">
                                {tq('ungroupedModels')}
                                <span className="ml-1.5 text-xs text-muted-foreground font-normal">
                                    ({filteredModels.length})
                                </span>
                            </span>
                        </div>
                        {isLoading ? (
                            <div className="flex-1 flex items-center justify-center text-muted-foreground text-sm">
                                加载中...
                            </div>
                        ) : filteredModels.length === 0 ? (
                            <div className="flex-1 flex items-center justify-center text-muted-foreground text-sm">
                                {tq('noMatch')}
                            </div>
                        ) : useVirtualList ? (
                            // 虚拟滚动模式：大量模型时仅渲染可见区域
                            <div ref={listScrollRef} className="flex-1 min-h-0 overflow-y-auto">
                                <div style={{ height: `${virtualizer.getTotalSize()}px`, position: 'relative' }}>
                                    {virtualizer.getVirtualItems().map((virtualRow) => {
                                        const m = filteredModels[virtualRow.index];
                                        return (
                                            <div
                                                key={`${m.channel_id}-${m.name}`}
                                                style={{
                                                    position: 'absolute',
                                                    top: 0,
                                                    left: 0,
                                                    width: '100%',
                                                    transform: `translateY(${virtualRow.start}px)`,
                                                }}
                                                ref={virtualizer.measureElement}
                                                data-index={virtualRow.index}
                                            >
                                                <ModelItem
                                                    model={m}
                                                    selected={m.name in modelConfigs}
                                                    onToggle={() => toggleModel(m.name)}
                                                />
                                            </div>
                                        );
                                    })}
                                </div>
                            </div>
                        ) : (
                            // 普通模式：少量模型时直接渲染
                            <div className="flex-1 min-h-0 overflow-y-auto">
                                {filteredModels.map((m) => (
                                    <ModelItem
                                        key={`${m.channel_id}-${m.name}`}
                                        model={m}
                                        selected={m.name in modelConfigs}
                                        onToggle={() => toggleModel(m.name)}
                                    />
                                ))}
                            </div>
                        )}
                    </div>

                    {/* 右侧：已选模型配置 */}
                    <div className="flex flex-col min-h-0 rounded-xl border border-border/50 bg-muted/30 overflow-hidden">
                        <div className="px-3 py-2 border-b border-border/30 bg-muted/50 shrink-0">
                            <span className="text-sm font-medium">
                                {tq('selectedConfig')}
                                {selectedNames.length > 0 && (
                                    <span className="ml-1.5 text-xs text-muted-foreground font-normal">
                                        ({selectedNames.length})
                                    </span>
                                )}
                            </span>
                        </div>
                        <div className="flex-1 min-h-0 overflow-y-auto p-2 space-y-2">
                            {selectedNames.length === 0 ? (
                                <div className="flex h-full items-center justify-center text-muted-foreground text-sm px-4 text-center">
                                    {tq('configHint')}
                                </div>
                            ) : (
                                selectedNames.map((name) => (
                                    <ModelConfigCard
                                        key={name}
                                        name={name}
                                        config={modelConfigs[name]}
                                        useAutoRegex={useAutoRegex}
                                        onModeChange={(mode) => setMode(name, mode)}
                                        onAddExclude={(term) => addExclude(name, term)}
                                        onRemoveExclude={(index) => removeExclude(name, index)}
                                        onRemove={() => removeModel(name)}
                                        onRegexChange={(regex) => setMatchRegex(name, regex)}
                                    />
                                ))
                            )}
                        </div>
                    </div>
                </div>
            </MorphingDialogDescription>

            {/* 底部操作栏 */}
            <div className="pt-3 mt-auto shrink-0 flex items-center justify-between">
                <span className="text-sm text-muted-foreground">
                    {selectedNames.length > 0
                        ? tq('selectedCount', { count: selectedNames.length })
                        : ''}
                </span>
                <div className="flex gap-2">
                    <Button
                        type="button"
                        variant="secondary"
                        className="rounded-xl h-10"
                        onClick={() => setIsOpen(false)}
                    >
                        {t('detail.actions.cancel')}
                    </Button>
                    <Button
                        type="button"
                        className="rounded-xl h-10"
                        disabled={selectedNames.length === 0 || batchCreate.isPending || submitted}
                        onClick={handleBatchCreate}
                    >
                        {(batchCreate.isPending || submitted) ? tq('creating') : `${tq('batchCreate')} (${selectedNames.length})`}
                    </Button>
                </div>
            </div>
        </div>
    );
}
