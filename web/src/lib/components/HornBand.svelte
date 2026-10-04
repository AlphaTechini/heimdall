<script lang="ts">
	import { toneRank, type Tone } from '$lib/band';
	import type { Snippet } from 'svelte';

	let { word, tone, children }: { word: string; tone: Tone; children?: Snippet } = $props();

	// Band and text colors are chosen for WCAG AA: white on calm-dark 5.3, ink on amber 4.7,
	// white on ember 5.8, white on fjord 6.8, white on granite-dark 5.6.
	const styles: Record<Tone, string> = {
		calm: 'bg-calm-dark text-white',
		warning: 'bg-amber text-ink',
		critical: 'bg-ember text-white',
		exiting: 'bg-ember-dark text-white',
		safe: 'bg-fjord text-white',
		unknown: 'bg-granite-dark text-white'
	};

	let sweeping = $state(false);
	let previous = -1;

	// One sweep when status escalates after the card is already on screen. Never on first render.
	$effect(() => {
		const rank = toneRank(tone);
		if (previous >= 0 && rank > previous) sweeping = true;
		previous = rank;
	});
</script>

<div class="relative overflow-hidden px-6 pt-6 pb-5 sm:px-7 {styles[tone]}">
	{#if sweeping}
		<span
			class="sweep pointer-events-none absolute inset-y-0 left-0 w-1/5 bg-white/30"
			aria-hidden="true"
			onanimationend={() => (sweeping = false)}
		></span>
	{/if}
	<p class="text-[2.75rem] display sm:text-[3.5rem]" role="status" aria-live="polite">
		{word}
	</p>
	{#if children}
		<div class="mt-3 max-w-[60ch] text-[0.925rem] leading-snug">{@render children()}</div>
	{/if}
</div>

<style>
	.sweep {
		animation: horn-sweep 900ms ease-out 1 both;
	}
</style>
