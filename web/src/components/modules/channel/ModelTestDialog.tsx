import { useEffect, useMemo, useState, type ReactNode } from 'react';
import { Activity, CheckCircle2, FlaskConical, Key, XCircle } from 'lucide-react';
import { useTranslations } from 'next-intl';
import { type Channel, ChannelType, useTestChannelModel } from '@/api/endpoints/channel';
import { Button } from '@/components/ui/button';
import {
    Dialog,
    DialogContent,
    DialogDescription,
    DialogFooter,
    DialogHeader,
    DialogTitle,
} from '@/components/ui/dialog';
import {
    Select,
    SelectContent,
    SelectItem,
    SelectTrigger,
    SelectValue,
} from '@/components/ui/select';
import { Switch } from '@/components/ui/switch';
import { cn } from '@/lib/utils';

type ModelTestDialogProps = {
    channel: Channel;
    open: boolean;
    onOpenChange: (open: boolean) => void;
};

const splitModels = (models: string) =>
    models
        .split(',')
        .map((item) => item.trim())
        .filter(Boolean);

const chatChannelTypes = new Set<ChannelType>([
    ChannelType.OpenAIChat,
    ChannelType.OpenAIResponse,
    ChannelType.Anthropic,
    ChannelType.Gemini,
    ChannelType.Volcengine,
]);

// 为 Key 选择器构造展示文本：有备注时附带备注，否则仅展示 ID。
// 与渠道详情/编辑弹层保持 `ID: xxx` 的命名约定，便于和日志中的 key_id 对应。
function keyLabel(key: { id: number; remark?: string }) {
    const remark = key.remark?.trim();
    return remark ? `ID: ${key.id} (${remark})` : `ID: ${key.id}`;
}

export function ModelTestDialog({ channel, open, onOpenChange }: ModelTestDialogProps) {
    const t = useTranslations('channel.modelTest');
    const testChannelModel = useTestChannelModel();
    const [selectedModel, setSelectedModel] = useState('');
    const [selectedKeyId, setSelectedKeyId] = useState<number | null>(null);
    // 允许失败时其他 Key 接力。开关 ON 时不传 key_id，由后端按渠道默认负载策略 + 多 Key 重试自然工作。
    // 这一行为依赖渠道的 enable_multi_key_retry 配置：如果渠道本身没开多 Key 重试，开启此项也只会
    // 让"起点 Key"由 balancer 决定，单次失败仍然不会触发回退。
    const [allowKeyFallback, setAllowKeyFallback] = useState(false);
    const resetTestState = testChannelModel.reset;

    const models = useMemo(() => {
        const seen = new Set<string>();
        const merged = [...splitModels(channel.model), ...splitModels(channel.custom_model)];
        return merged.filter((model) => {
            if (seen.has(model)) return false;
            seen.add(model);
            return true;
        });
    }, [channel.custom_model, channel.model]);

    // 仅展示已启用且有内容的 Key，禁用 Key 不应被用户作为测试对象。
    const enabledKeys = useMemo(
        () => (channel.keys ?? []).filter((k) => k.enabled && k.channel_key),
        [channel.keys],
    );

    const isChatChannel = chatChannelTypes.has(channel.type);
    const activeModel = models.includes(selectedModel) ? selectedModel : (models[0] ?? '');
    const canTest = isChatChannel && channel.enabled && models.length > 0;
    const result = testChannelModel.data;
    const errorMessage = testChannelModel.error?.message ?? '';

    // Effect 1：仅在弹层 open 状态切换时执行——打开时清掉上次的测试结果，关闭时清掉选中 Key 和接力开关。
    // 不依赖 enabledKeys，避免渠道列表 refetch 触发 channel.keys 引用变化时把测试结果误清空
    // （useTestChannelModel.onSettled 会 invalidate ['channels', 'list']，从而带来新的 channel 引用）。
    useEffect(() => {
        if (!open) {
            setSelectedKeyId(null);
            setAllowKeyFallback(false);
            return;
        }
        resetTestState();
    }, [open, resetTestState]);

    // Effect 2：保证打开状态下始终有一个有效的默认选中 Key。
    // 加 currentValid 守卫：当前选中的 Key 仍存在于 enabledKeys 时不动，避免引用变化导致无谓的 setState；
    // 仅当未选 / 选中已失效 / 新增 Key 等真实变化时才回到第一个 Key。
    useEffect(() => {
        if (!open) return;
        const currentValid = selectedKeyId != null && enabledKeys.some((k) => k.id === selectedKeyId);
        if (!currentValid && enabledKeys.length > 0) {
            setSelectedKeyId(enabledKeys[0].id);
        }
    }, [open, selectedKeyId, enabledKeys]);

    const disabledReason = useMemo(() => {
        if (!isChatChannel) return t('unsupportedType');
        if (!channel.enabled) return t('disabledChannel');
        if (models.length === 0) return t('noModels');
        return '';
    }, [channel.enabled, isChatChannel, models.length, t]);

    const handleSubmit = () => {
        if (!activeModel) return;
        testChannelModel.mutate({
            channel_id: channel.id,
            model: activeModel,
            // 接力开关 ON 时不传 key_id，让后端按渠道默认负载策略 + 多 Key 重试自然工作；
            // OFF 时传选中的 key_id，由 handler 收敛 channel.Keys 为单一 Key 做精确诊断。
            key_id: allowKeyFallback ? undefined : (selectedKeyId ?? undefined),
        });
    };

    return (
        <Dialog open={open} onOpenChange={onOpenChange}>
            <DialogContent className="sm:max-w-md">
                <DialogHeader>
                    <DialogTitle className="flex items-center gap-2">
                        <FlaskConical className="size-5 text-primary" />
                        {t('title')}
                    </DialogTitle>
                    <DialogDescription>{channel.name}</DialogDescription>
                </DialogHeader>

                <div className="space-y-4">
                    <div className="space-y-2">
                        <label className="text-sm font-medium text-foreground">{t('model')}</label>
                        <Select
                            value={activeModel}
                            onValueChange={(value) => {
                                setSelectedModel(value);
                                resetTestState();
                            }}
                            disabled={!canTest || testChannelModel.isPending}
                        >
                            <SelectTrigger className="w-full">
                                <SelectValue placeholder={t('selectModel')} />
                            </SelectTrigger>
                            <SelectContent>
                                {models.map((model) => (
                                    <SelectItem key={model} value={model}>
                                        {model}
                                    </SelectItem>
                                ))}
                            </SelectContent>
                        </Select>
                    </div>

                    {/* Key 选择器：允许测试时指定使用渠道下具体的某一个 Key，
                        便于多 Key 渠道在出问题时精准定位是哪一把 Key 不可用。 */}
                    <div className="space-y-2">
                        <label className="flex items-center gap-1.5 text-sm font-medium text-foreground">
                            <Key className="size-3.5 text-muted-foreground" />
                            {t('key')}
                        </label>
                        <Select
                            value={selectedKeyId != null ? String(selectedKeyId) : ''}
                            onValueChange={(value) => {
                                setSelectedKeyId(Number(value));
                                resetTestState();
                            }}
                            disabled={!canTest || testChannelModel.isPending || enabledKeys.length === 0 || allowKeyFallback}
                        >
                            <SelectTrigger className="w-full">
                                <SelectValue placeholder={t('selectKey')} />
                            </SelectTrigger>
                            <SelectContent>
                                {enabledKeys.map((k) => (
                                    <SelectItem key={k.id} value={String(k.id)}>
                                        {keyLabel(k)}
                                    </SelectItem>
                                ))}
                            </SelectContent>
                        </Select>
                        {/* 接力开关：ON 时不传 key_id，由后端按渠道默认负载策略 + 多 Key 重试自然工作。
                            放在 Key 选择器下方而非外层，是因为这两个控件语义强相关：开关决定 Key 选择器是否生效。 */}
                        <div className="flex items-start justify-between gap-3 rounded-md border border-border/60 bg-muted/30 p-2.5">
                            <div className="min-w-0 flex-1 space-y-0.5">
                                <p className="text-sm font-medium text-foreground">{t('fallback')}</p>
                                <p className="text-xs text-muted-foreground">
                                    {allowKeyFallback ? t('fallbackOnHint') : t('fallbackOffHint')}
                                </p>
                            </div>
                            <Switch
                                checked={allowKeyFallback}
                                onCheckedChange={(checked) => {
                                    setAllowKeyFallback(checked);
                                    resetTestState();
                                }}
                                disabled={!canTest || testChannelModel.isPending}
                            />
                        </div>
                        {enabledKeys.length === 0 && canTest && (
                            <StatusBox tone="warning" icon={<XCircle className="size-4" />}>
                                {t('noKeys')}
                            </StatusBox>
                        )}
                    </div>

                    {disabledReason && (
                        <StatusBox tone="warning" icon={<XCircle className="size-4" />}>
                            {disabledReason}
                        </StatusBox>
                    )}

                    {testChannelModel.isPending && (
                        <StatusBox tone="loading" icon={<Activity className="size-4 animate-pulse" />}>
                            {t('testing')}
                        </StatusBox>
                    )}

                    {result && (
                        <StatusBox tone="success" icon={<CheckCircle2 className="size-4" />}>
                            {t('success', { model: result.model, duration: result.duration_ms })}
                        </StatusBox>
                    )}

                    {errorMessage && !testChannelModel.isPending && (
                        <StatusBox tone="error" icon={<XCircle className="size-4" />}>
                            {errorMessage}
                        </StatusBox>
                    )}
                </div>

                <DialogFooter>
                    <Button variant="secondary" onClick={() => onOpenChange(false)}>
                        {t('close')}
                    </Button>
                    <Button onClick={handleSubmit} disabled={!canTest || !activeModel || testChannelModel.isPending}>
                        {testChannelModel.isPending ? t('testingButton') : t('start')}
                    </Button>
                </DialogFooter>
            </DialogContent>
        </Dialog>
    );
}

function StatusBox({ tone, icon, children }: { tone: 'loading' | 'success' | 'error' | 'warning'; icon: ReactNode; children: ReactNode }) {
    return (
        <div
            className={cn(
                // max-w-full + overflow-hidden 防止子内容把容器撑出 Dialog 边界
                'flex items-start gap-2 rounded-lg border p-3 text-sm max-w-full overflow-hidden',
                tone === 'loading' && 'border-primary/30 bg-primary/5 text-primary',
                tone === 'success' && 'border-emerald-500/30 bg-emerald-500/10 text-emerald-700 dark:text-emerald-300',
                tone === 'error' && 'border-destructive/30 bg-destructive/10 text-destructive',
                tone === 'warning' && 'border-amber-500/30 bg-amber-500/10 text-amber-700 dark:text-amber-300',
            )}
        >
            <span className="mt-0.5 shrink-0">{icon}</span>
            {/* break-all + wrap-anywhere：错误信息常含长 URL / 无空格字符串，
                必须按字符强制换行，否则会突破 flex 子项的 min-w-0 限制把弹框撑宽。
                Tailwind v4 中 wrap-anywhere 对应 overflow-wrap: anywhere。 */}
            <span className="min-w-0 break-all overflow-hidden wrap-anywhere">{children}</span>
        </div>
    );
}
