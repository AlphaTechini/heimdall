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
		...(app.backtests.length > 0
			? [
					{
						href: resolve('/backtest'),
						label: 'Backtest',
						match: (p: string) => p.startsWith('/backtest')
					}
				]
			: []),
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

<header class="bg-ink text-white">
	<div class="mx-auto flex max-w-6xl flex-wrap items-center gap-x-8 gap-y-1 px-4 py-3 sm:px-6">
		<a
			href={resolve('/')}
			class="inline-flex items-center gap-2.5 text-white hover:text-white"
			aria-label="Heimdall, the exit guard for Arbitrum depositors, home"
		>
			<svg width="22" height="22" viewBox="0 0 24 24" fill="none" aria-hidden="true">
				<path d="M3.5 21 V11 a8.5 8.5 0 0 1 17 0 V21" stroke="currentColor" stroke-width="2" />
				<path d="M8.5 21 V12.5 a3.5 3.5 0 0 1 7 0 V21" stroke="#8fc1e3" stroke-width="2" />
			</svg>
			<span class="text-[1.35rem] display">Heimdall</span>
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
					class="rounded-md px-3 py-1.5 text-[0.95rem] font-medium no-underline transition-colors {active
						? 'bg-white/12 text-white hover:text-white'
						: 'text-nav hover:bg-white/6 hover:text-white'}"
				>
					{item.label}
				</a>
			{/each}
		</nav>

		<div class="order-2 ml-auto flex items-center gap-3 sm:order-3">
			{#if wallet.connected && app.config}
				{#if wallet.wrongNetwork}
					<span class="hidden text-sm text-white xs:inline">Wrong network</span>
					<button
						type="button"
						class="btn btn-sm bg-white text-ink hover:bg-frost"
						disabled={wallet.switching}
						onclick={() => wallet.switchNetwork()}
					>
						{wallet.switching ? 'Switching' : 'Switch network'}
					</button>
				{:else}
					<span class="hidden items-center gap-2 text-sm text-nav md:inline-flex"
						><span class="size-2 rounded-full bg-calm"></span>{app.config.chainName}</span
					>
				{/if}
			{/if}

			{#if wallet.address}
				<div class="relative" bind:this={menuRoot}>
					<button
						type="button"
						bind:this={menuButton}
						class="inline-flex items-center gap-2 rounded-lg border border-white/15 bg-white/8 px-3 py-1.5 text-sm font-medium text-white tabular transition-colors hover:bg-white/14"
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
							class="absolute right-0 z-30 mt-2 w-72 max-w-[calc(100vw-2rem)] rounded-xl border border-line bg-white p-1.5 text-ink shadow-[0_12px_32px_-12px_rgba(21,35,45,0.35)]"
						>
							<p class="px-3 py-2 text-xs break-all text-granite-dark tabular">{wallet.address}</p>
							<button
								type="button"
								role="menuitem"
								class="block w-full rounded-lg px-3 py-2 text-left text-sm hover:bg-frost"
								onclick={copyAddress}
							>
								Copy address
							</button>
							<button
								type="button"
								role="menuitem"
								class="block w-full rounded-lg px-3 py-2 text-left text-sm hover:bg-frost"
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
					class="btn btn-sm bg-white text-ink hover:bg-frost"
					disabled={wallet.connecting}
					onclick={() => wallet.connect()}
				>
					{wallet.connecting ? 'Waiting for wallet' : 'Connect wallet'}
				</button>
			{/if}
		</div>
	</div>
	{#if wallet.notice}
		<p class="border-t border-white/10 bg-ink-2 px-4 py-2 text-sm text-white sm:px-6" role="status">
			<span class="mx-auto block max-w-6xl">{wallet.notice}</span>
		</p>
	{/if}
</header>
