<script lang="ts">
	import { resolve } from '$app/paths';
	import { page } from '$app/state';
	import { app } from '$lib/app.svelte';
	import { auth } from '$lib/auth.svelte';
	import { shortAddress } from '$lib/format';
	import { toasts } from '$lib/toast.svelte';
	import { wallet } from '$lib/wallet.svelte';
	import Icon from './Icon.svelte';

	const nav = $derived([
		{
			href: resolve('/'),
			label: 'Dashboard',
			match: (p: string) => p === '/' || p.startsWith('/positions')
		},
		{
			href: resolve('/activity'),
			label: 'Activity',
			match: (p: string) => p.startsWith('/activity')
		},
		{
			href: resolve('/settings'),
			label: 'Settings',
			match: (p: string) => p.startsWith('/settings')
		},
		...(app.config?.demoMode
			? [
					{
						href: resolve('/simulator'),
						label: 'Simulator',
						match: (p: string) => p.startsWith('/simulator')
					}
				]
			: [])
	]);

	let menuOpen = $state(false);
	let menuRoot = $state<HTMLElement | null>(null);
	let menuButton = $state<HTMLButtonElement | null>(null);

	function onWindowClick(e: MouseEvent) {
		if (menuOpen && menuRoot && !menuRoot.contains(e.target as Node)) menuOpen = false;
	}
	function onKeydown(e: KeyboardEvent) {
		if (e.key === 'Escape' && menuOpen) {
			menuOpen = false;
			menuButton?.focus();
		}
	}

	async function copyAddress() {
		if (!wallet.address) return;
		try {
			await navigator.clipboard.writeText(wallet.address);
			toasts.show('Address copied');
		} catch {
			toasts.show('Could not copy. Select the address in the menu and copy it yourself.');
		}
		menuOpen = false;
	}

	function disconnect() {
		wallet.disconnect();
		auth.clear();
		menuOpen = false;
	}
</script>

<svelte:window onclick={onWindowClick} onkeydown={onKeydown} />

<header class="border-b border-line bg-white">
	<div class="mx-auto flex max-w-6xl flex-wrap items-center gap-x-6 gap-y-2 px-4 py-3 sm:px-6">
		<a
			href={resolve('/')}
			class="text-2xl display text-ink hover:text-ink"
			aria-label="Heimdall, the exit guard for Arbitrum depositors, home"
		>
			Heimdall
		</a>

		<nav
			aria-label="Main"
			class="order-3 -mx-1 flex w-full flex-wrap gap-x-1 sm:order-2 sm:mx-0 sm:w-auto"
		>
			{#each nav as item (item.href)}
				{@const active = item.match(page.url.pathname)}
				<a
					href={item.href}
					aria-current={active ? 'page' : undefined}
					class="border-b-2 px-2 py-2 text-[0.95rem] font-medium {active
						? 'border-fjord text-ink'
						: 'border-transparent text-granite-dark hover:text-ink'}"
				>
					{item.label}
				</a>
			{/each}
		</nav>

		<div class="order-2 ml-auto flex items-center gap-3 sm:order-3">
			{#if wallet.connected && app.config}
				{#if wallet.wrongNetwork}
					<span class="hidden text-sm text-ink xs:inline">Wrong network</span>
					<button
						type="button"
						class="rounded-md border border-fjord px-3 py-1.5 text-sm font-medium text-fjord hover:bg-frost disabled:opacity-60"
						disabled={wallet.switching}
						onclick={() => wallet.switchNetwork()}
					>
						{wallet.switching ? 'Switching' : 'Switch network'}
					</button>
				{:else}
					<span class="hidden text-sm text-granite-dark md:inline">{app.config.chainName}</span>
				{/if}
			{/if}

			{#if wallet.address}
				<div class="relative" bind:this={menuRoot}>
					<button
						type="button"
						bind:this={menuButton}
						class="inline-flex items-center gap-2 rounded-md border border-granite bg-white px-3 py-1.5 text-sm font-medium tabular hover:bg-frost"
						aria-haspopup="menu"
						aria-expanded={menuOpen}
						onclick={() => (menuOpen = !menuOpen)}
					>
						{shortAddress(wallet.address)}
						<Icon name="chevron" size={14} />
					</button>
					{#if menuOpen}
						<div
							role="menu"
							aria-label="Wallet"
							class="absolute right-0 z-30 mt-2 w-72 max-w-[calc(100vw-2rem)] rounded-md border border-line bg-white p-2 shadow-lg"
						>
							<p class="px-3 py-2 text-xs break-all text-granite-dark tabular">{wallet.address}</p>
							<button
								type="button"
								role="menuitem"
								class="block w-full rounded px-3 py-2 text-left text-sm hover:bg-frost"
								onclick={copyAddress}
							>
								Copy address
							</button>
							<button
								type="button"
								role="menuitem"
								class="block w-full rounded px-3 py-2 text-left text-sm hover:bg-frost"
								onclick={disconnect}
							>
								Disconnect
							</button>
						</div>
					{/if}
				</div>
			{:else}
				<button
					type="button"
					class="rounded-md bg-fjord px-4 py-2 text-sm font-medium text-white hover:bg-fjord-dark disabled:opacity-60"
					disabled={wallet.connecting}
					onclick={() => wallet.connect()}
				>
					{wallet.connecting ? 'Waiting for wallet' : 'Connect wallet'}
				</button>
			{/if}
		</div>
	</div>
	{#if wallet.notice}
		<p class="border-t border-line bg-frost px-4 py-2 text-sm text-ink sm:px-6" role="status">
			<span class="mx-auto block max-w-6xl">{wallet.notice}</span>
		</p>
	{/if}
</header>
