<script lang="ts">
	import { formatClock, formatReading } from '$lib/format';
	import type { HistoryPoint, SignalLevel } from '$lib/types';

	let {
		label,
		tooltip,
		unit,
		id,
		points,
		threshold
	}: {
		label: string;
		tooltip: string;
		unit: string;
		id: string;
		points: HistoryPoint[];
		threshold: { warning?: number; critical?: number } | undefined;
	} = $props();

	const W = 320;
	const H = 128;
	const PAD = { l: 8, r: 8, t: 14, b: 20 };

	interface Sample {
		x: number;
		y: number;
		v: number;
		level: SignalLevel;
		time: string;
		block: number;
	}

	const series = $derived(
		points
			.map((p) => {
				const s = p.signals.find((x) => x.id === id);
				return s
					? { v: s.value, level: s.level, time: p.time, block: p.block, t: Date.parse(p.time) }
					: null;
			})
			.filter(
				(s): s is NonNullable<typeof s> =>
					s !== null && s.level !== 'unavailable' && Number.isFinite(s.v)
			)
	);

	const scale = $derived.by(() => {
		const values = series.map((s) => s.v);
		if (threshold?.warning !== undefined) values.push(threshold.warning);
		if (threshold?.critical !== undefined) values.push(threshold.critical);
		let lo = Math.min(...values);
		let hi = Math.max(...values);
		if (!Number.isFinite(lo)) return null;
		if (hi === lo) {
			lo -= 1;
			hi += 1;
		}
		const pad = (hi - lo) * 0.08;
		const t0 = series[0]?.t ?? 0;
		const t1 = series[series.length - 1]?.t ?? 1;
		return { lo: lo - pad, hi: hi + pad, t0, t1: t1 === t0 ? t0 + 1 : t1 };
	});

	const samples = $derived.by((): Sample[] => {
		const sc = scale;
		if (!sc) return [];
		return series.map((s) => ({
			x: PAD.l + ((s.t - sc.t0) / (sc.t1 - sc.t0)) * (W - PAD.l - PAD.r),
			y: PAD.t + (1 - (s.v - sc.lo) / (sc.hi - sc.lo)) * (H - PAD.t - PAD.b),
			v: s.v,
			level: s.level,
			time: s.time,
			block: s.block
		}));
	});

	const yOf = (v: number) => {
		const sc = scale;
		return sc ? PAD.t + (1 - (v - sc.lo) / (sc.hi - sc.lo)) * (H - PAD.t - PAD.b) : 0;
	};

	const path = $derived(
		samples.map((s, i) => `${i === 0 ? 'M' : 'L'}${s.x.toFixed(1)} ${s.y.toFixed(1)}`).join(' ')
	);
	const flagged = $derived(samples.filter((s) => s.level === 'warning' || s.level === 'critical'));

	let hoverIndex = $state<number | null>(null);
	const hovered = $derived(hoverIndex !== null ? samples[hoverIndex] : null);
	const latest = $derived(samples[samples.length - 1]);

	function onMove(e: PointerEvent) {
		if (samples.length === 0) return;
		const rect = (e.currentTarget as SVGElement).getBoundingClientRect();
		const x = ((e.clientX - rect.left) / rect.width) * W;
		let best = 0;
		for (let i = 1; i < samples.length; i++) {
			if (Math.abs(samples[i].x - x) < Math.abs(samples[best].x - x)) best = i;
		}
		hoverIndex = best;
	}

	const LEVEL_TEXT: Record<SignalLevel, string> = {
		ok: 'Normal',
		warning: 'Warning',
		critical: 'Critical',
		unavailable: 'Not available'
	};
</script>

<figure class="min-w-0 panel px-5 py-4">
	<figcaption class="flex flex-wrap items-baseline justify-between gap-x-3">
		<span class="font-semibold" title={tooltip}>{label}</span>
		<span class="text-sm text-granite-dark tabular">
			{#if hovered}
				{formatReading(hovered.v)} {unit}, {formatClock(hovered.time)}, {LEVEL_TEXT[hovered.level]}
			{:else if latest}
				Latest {formatReading(latest.v)} {unit}
			{:else}
				No readings yet
			{/if}
		</span>
	</figcaption>
	{#if samples.length > 0 && scale}
		<svg
			viewBox="0 0 {W} {H}"
			class="mt-1 block w-full touch-pan-y"
			role="img"
			aria-label="{label} over time. Latest {latest
				? formatReading(latest.v)
				: ''} {unit}.{threshold?.warning !== undefined
				? ` Warning at ${formatReading(threshold.warning)}.`
				: ''}{threshold?.critical !== undefined
				? ` Critical at ${formatReading(threshold.critical)}.`
				: ''}"
			onpointermove={onMove}
			onpointerleave={() => (hoverIndex = null)}
		>
			<line
				x1={PAD.l}
				x2={W - PAD.r}
				y1={H - PAD.b}
				y2={H - PAD.b}
				stroke="var(--color-line)"
				stroke-width="1"
			/>
			{#if threshold?.warning !== undefined}
				<line
					x1={PAD.l}
					x2={W - PAD.r}
					y1={yOf(threshold.warning)}
					y2={yOf(threshold.warning)}
					stroke="var(--color-amber)"
					stroke-width="1"
					stroke-dasharray="4 3"
				/>
				<text x={PAD.l} y={yOf(threshold.warning) - 3} font-size="10" fill="var(--color-ink)">
					Warning at {formatReading(threshold.warning)}
				</text>
			{/if}
			{#if threshold?.critical !== undefined}
				<line
					x1={PAD.l}
					x2={W - PAD.r}
					y1={yOf(threshold.critical)}
					y2={yOf(threshold.critical)}
					stroke="var(--color-ember)"
					stroke-width="1"
					stroke-dasharray="4 3"
				/>
				<text x={PAD.l} y={yOf(threshold.critical) - 3} font-size="10" fill="var(--color-ink)">
					Critical at {formatReading(threshold.critical)}
				</text>
			{/if}
			{#if samples.length > 1}
				<path
					d={path}
					fill="none"
					stroke="var(--color-fjord)"
					stroke-width="2"
					stroke-linejoin="round"
					stroke-linecap="round"
				/>
			{:else}
				<circle cx={samples[0].x} cy={samples[0].y} r="4" fill="var(--color-fjord)" />
			{/if}
			{#each flagged as s, i (i)}
				<circle
					cx={s.x}
					cy={s.y}
					r="4"
					fill={s.level === 'critical' ? 'var(--color-ember)' : 'var(--color-amber)'}
					stroke="#fff"
					stroke-width="2"
				/>
			{/each}
			{#if hovered}
				<line
					x1={hovered.x}
					x2={hovered.x}
					y1={PAD.t}
					y2={H - PAD.b}
					stroke="var(--color-granite)"
					stroke-width="1"
				/>
				<circle
					cx={hovered.x}
					cy={hovered.y}
					r="4"
					fill="var(--color-fjord)"
					stroke="#fff"
					stroke-width="2"
				/>
			{/if}
			<text x={PAD.l} y={H - 6} font-size="10" fill="var(--color-granite-dark)"
				>{formatClock(series[0].time)}</text
			>
			<text
				x={W - PAD.r}
				y={H - 6}
				font-size="10"
				text-anchor="end"
				fill="var(--color-granite-dark)"
			>
				{formatClock(series[series.length - 1].time)}
			</text>
		</svg>
	{:else}
		<p class="mt-2 text-sm text-granite-dark">
			No readings for this signal. It has no data source for this vault yet.
		</p>
	{/if}
</figure>
