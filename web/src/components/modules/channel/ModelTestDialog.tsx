import { useEffect, useMemo, useState, type ReactNode } from 'react';
import { Activity, CheckCircle2, FlaskConical, XCircle } from 'lucide-react';
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

export function ModelTestDialog({ channel, open, onOpenChange }: ModelTestDialogProps) {
    const t = useTranslations('channel.modelTest');
    const testChannelModel = useTestChannelModel();
    const [selectedModel, setSelectedModel] = useState('');
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

    const isChatChannel = chatChannelTypes.has(channel.type);
    const activeModel = models.includes(selectedModel) ? selectedModel : (models[0] ?? '');
    const canTest = isChatChannel && channel.enabled && models.length > 0;
    const result = testChannelModel.data;
    const errorMessage = testChannelModel.error?.message ?? '';

    useEffect(() => {
        if (!open) return;
        resetTestState();
    }, [open, resetTestState]);

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
                'flex items-start gap-2 rounded-lg border p-3 text-sm',
                tone === 'loading' && 'border-primary/30 bg-primary/5 text-primary',
                tone === 'success' && 'border-emerald-500/30 bg-emerald-500/10 text-emerald-700 dark:text-emerald-300',
                tone === 'error' && 'border-destructive/30 bg-destructive/10 text-destructive',
                tone === 'warning' && 'border-amber-500/30 bg-amber-500/10 text-amber-700 dark:text-amber-300',
            )}
        >
            <span className="mt-0.5 shrink-0">{icon}</span>
            <span className="min-w-0 break-words">{children}</span>
        </div>
    );
}
