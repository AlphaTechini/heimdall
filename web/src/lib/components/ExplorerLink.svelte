<script lang="ts">
	import type { Link } from '$lib/explorer';
	import Icon from './Icon.svelte';
	import type { Snippet } from 'svelte';

	let {
		link,
		children,
		class: cls = ''
	}: { link: Link; children: Snippet; class?: string } = $props();
</script>

{#if link.external}
	<!-- eslint-disable svelte/no-navigation-without-resolve -- external explorer URL -->
	<a
		href={link.href}
		target="_blank"
		rel="noopener noreferrer"
		class="inline-flex items-center gap-1 {cls}"
	>
		{@render children()}
		<Icon name="external" size={13} />
		<span class="sr-only">(opens in a new tab)</span>
	</a>
	<!-- eslint-enable svelte/no-navigation-without-resolve -->
{:else}
	<!-- eslint-disable-next-line svelte/no-navigation-without-resolve -- already resolved by txLink() -->
	<a href={link.href} class={cls}>{@render children()}</a>
{/if}
