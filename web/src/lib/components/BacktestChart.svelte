<script lang="ts">
	import { formatClock, formatNumber, shortAddress } from '$lib/format';
	import type { Backtest } from '$lib/types';

	let { data }: { data: Backtest } = $props();

	// The chart is drawn at its real pixel width so text stays readable on a phone.
	let width = $state(720);
	const W = $derived(Math.max(300, width));
	const H = 300;
	const PAD = { l: 44, r: 16, t: 44, b: 34 };
	// Tokens are told apart by color and line style (never color alone). None of these is a severity color.
	const STYLES = [
		{ color: 'var(--color-fjord)', dash: '' },
		{ color: '#6b4e9b', dash: '7 4' },
		{ color: '#157a8c', dash: '2 3' },
		{ color: 'var(--color-granite-dark)', dash: '10 3 2 3' }
	];

	const tokens = $derived(data.tokens.length > 0 ? data.tokens : Object.keys(data.peakBalances));
	const first = $derived(data.points[0]?.block ?? data.fromBlock);
	const last = $derived(data.points[data.points.length - 1]?.block ?? data.toBlock);

	const xOf = (block: number) =>
		PAD.l + ((block - first) / Math.max(1, last - first)) * (W - PAD.l - PAD.r);
	const yOf = (pct: number) => PAD.t + (1 - pct / 100) * (H - PAD.t - PAD.b);

	/** Balance of one token as a percentage of its own peak over the replayed range. */
	function pctOfPeak(raw: string | undefined, peak: string | undefined): number | null {
		if (raw === undefined || !peak) return null;
		try {
			const p = BigInt(peak);
			if (p === 0n) return null;
			return Number((BigInt(raw) * 10000n) / p) / 100;
		} catch {
			return null;
		}
	}

	const series = $derived(
		tokens.map((token, i) => ({
			token,
			style: STYLES[i % STYLES.length],
			points: data.points
				.map((pt) => ({
					block: pt.block,
					time: pt.time,
					pct: pctOfPeak(pt.balances?.[token], data.peakBalances?.[token])
				}))
				.filter((p): p is { block: number; time: string; pct: number } => p.pct !== null)
		}))
	);

	const paths = $derived(
		series.map((s) => ({
			...s,
			d: s.points
				.map((p, i) => `${i === 0 ? 'M' : 'L'}${xOf(p.block).toFixed(1)} ${yOf(p.pct).toFixed(1)}`)
				.join(' ')
		}))
	);

	const markers = $derived(
		[
			data.firstWarningBlock !== null
				? { block: data.firstWarningBlock, label: 'Warning', color: 'var(--color-amber)' }
				: null,
			data.firstCriticalBlock !== null
				? { block: data.firstCriticalBlock, label: 'Critical', color: 'var(--color-ember)' }
				: null
		].filter((m): m is { block: number; label: string; color: string } => m !== null)
	);

	let hover = $state<number | null>(null);
	const blocks = $derived(data.points.map((p) => p.block));
	const hoverBlock = $derived(hover !== null ? blocks[hover] : null);
	const hoverPoint = $derived(hover !== null ? data.points[hover] : null);

	function onMove(e: PointerEvent) {
		if (blocks.length === 0) return;
		const rect = (e.currentTarget as SVGElement).getBoundingClientRect();
		const x = ((e.clientX - rect.left) / rect.width) * W;
		let best = 0;
		for (let i = 1; i < blocks.length; i++) {
			if (Math.abs(xOf(blocks[i]) - x) < Math.abs(xOf(blocks[best]) - x)) best = i;
		}
		hover = best;
	}

	const hoverText = $derived.by(() => {
		const pt = hoverPoint;
		if (!pt) return '';
		const shares = tokens.map((t) => {
			const v = pctOfPeak(pt.balances?.[t], data.peakBalances?.[t]);
			return v === null ? '?' : `${v.toFixed(1)}%`;
		});
		const risk =
			pt.severity === 'watch' ? 'Calm' : pt.severity === 'warning' ? 'Warning' : 'Critical';
		return `Block ${pt.block}, ${formatClock(pt.time)}: ${shares.join(' and ')} of peak. Risk: ${risk}.`;
	});

	const label = (token: string) => data.tokenSymbols?.[token] ?? `Token ${shortAddress(token)}`;
	const lowest = (s: (typeof series)[number]) => Math.min(...s.points.map((p) => p.pct));
</script>

<figure bind:clientWidth={width}>
	<ul class="mb-2 flex flex-wrap gap-x-6 gap-y-1 text-sm">
		{#each paths as s (s.token)}
			<li class="flex items-center gap-2 tabular">
				<svg width="28" height="10" aria-hidden="true">
					<line
						x1="0"
						x2="28"
						y1="5"
						y2="5"
						stroke={s.style.color}
						stroke-width="2.5"
						stroke-dasharray={s.style.dash}
					/>
				</svg>
				<span>{tokens.length > 1 ? label(s.token) : 'Balance in the pool'}</span>
				<span class="text-granite-dark">lowest {formatNumber(lowest(s), 1)}% of its peak</span>
			</li>
		{/each}
	</ul>
	<svg
		viewBox="0 0 {W} {H}"
		class="block w-full touch-pan-y"
		role="img"
		aria-label="Balance held by the pool over blocks {first} to {last}, as a percentage of its peak.{data.firstCriticalBlock !==
		null
			? ` Heimdall's rules reach Critical at block ${data.firstCriticalBlock}.`
			: ' The rules never reached Critical.'}"
		onpointermove={onMove}
		onpointerleave={() => (hover = null)}
	>
		{#each [0, 50, 100] as tick (tick)}
			<line
				x1={PAD.l}
				x2={W - PAD.r}
				y1={yOf(tick)}
				y2={yOf(tick)}
				stroke="var(--color-line)"
				stroke-width="1"
			/>
			<text
				x={PAD.l - 8}
				y={yOf(tick) + 4}
				font-size="12"
				text-anchor="end"
				fill="var(--color-granite-dark)">{tick}%</text
			>
		{/each}
		{#each markers as m (m.label)}
			<line
				x1={xOf(m.block)}
				x2={xOf(m.block)}
				y1={m.label === 'Critical' ? PAD.t - 22 : PAD.t - 38}
				y2={H - PAD.b}
				stroke={m.color}
				stroke-width="1.5"
				stroke-dasharray="5 3"
			/>
			<text
				x={m.label === 'Critical'
					? Math.min(xOf(m.block) + 6, W - 150)
					: Math.max(xOf(m.block) - 6, PAD.l + 180)}
				y={m.label === 'Critical' ? PAD.t - 10 : PAD.t - 26}
				text-anchor={m.label === 'Critical' ? 'start' : 'end'}
				font-size="12"
				fill="var(--color-ink)"
				stroke="#fff"
				stroke-width="4"
				paint-order="stroke"
			>
				{m.label} at block {m.block}
			</text>
		{/each}
		{#each paths as s (s.token)}
			<path
				d={s.d}
				fill="none"
				stroke={s.style.color}
				stroke-width="2"
				stroke-dasharray={s.style.dash}
				stroke-linejoin="round"
			/>
		{/each}
		{#if hoverBlock !== null}
			<line
				x1={xOf(hoverBlock)}
				x2={xOf(hoverBlock)}
				y1={PAD.t}
				y2={H - PAD.b}
				stroke="var(--color-granite)"
				stroke-width="1"
			/>
		{/if}
		<text x={PAD.l} y={H - 10} font-size="12" fill="var(--color-granite-dark)">Block {first}</text>
		<text x={W - PAD.r} y={H - 10} font-size="12" text-anchor="end" fill="var(--color-granite-dark)"
			>Block {last}</text
		>
	</svg>
	<figcaption class="mt-1 min-h-[1.5rem] text-sm text-granite-dark tabular" aria-live="off">
		{#if hoverText}
			{hoverText}
		{:else}
			Move over the chart to read a block.
		{/if}
	</figcaption>
</figure>
