<script lang="ts">
	import { onMount, untrack } from 'svelte';
	import { Action } from '$lib/action.svelte';
	import { api } from '$lib/api';
	import { app } from '$lib/app.svelte';
	import { auth } from '$lib/auth.svelte';
	import { describeError } from '$lib/errors';
	import { formatClock } from '$lib/format';
	import { defaultPolicy } from '$lib/policy';
	import { toasts } from '$lib/toast.svelte';
	import type { Policy, TelegramLink, UserSettings } from '$lib/types';
	import { wallet } from '$lib/wallet.svelte';
	import ActionStatus from '$lib/components/ActionStatus.svelte';
	import ErrorState from '$lib/components/ErrorState.svelte';
	import PolicyForm from '$lib/components/PolicyForm.svelte';
	import WalletNotice from '$lib/components/WalletNotice.svelte';

	const uid = $props.id();

	let settings = $state<UserSettings | null>(null);
	let status = $state<'idle' | 'loading' | 'ready' | 'error'>('idle');
	let error = $state('');

	async function loadSettings() {
		status = settings ? 'ready' : 'loading';
		try {
			const res = await auth.call((token) => api.settings(token));
			settings = res;
			status = 'ready';
			if (!emailTouched) emailInput = res.email?.address ?? '';
			if (res.email && !res.email.verified) codeSent = true;
			if (!policyTouched) policy = { ...res.defaultPolicy };
		} catch (e) {
			error = describeError(e, 'Could not load your settings.').message;
			status = 'error';
		}
	}

	// Load once the wallet is connected and signed in; clear when it disconnects.
	$effect(() => {
		const token = auth.token;
		const address = wallet.address;
		untrack(() => {
			if (token && address) void loadSettings();
			else {
				settings = null;
				status = 'idle';
			}
		});
	});

	const signIn = new Action('Could not sign in.', async (ctx) => {
		ctx.pending('Waiting for wallet signature');
		await auth.ensure();
		ctx.success('Signed in.');
	});

	// ---- Telegram ----
	let link = $state<TelegramLink | null>(null);
	const telegramLinked = $derived(settings?.telegram?.linked === true);

	const connectTelegram = new Action('Could not start the Telegram link.', async (ctx) => {
		ctx.pending('Asking the server for a link code');
		link = await auth.call((t) => api.telegramLink(t));
		ctx.success('Open Telegram and press Start. This page updates when the link works.');
	});
	const disconnectTelegram = new Action('Could not disconnect Telegram.', async (ctx) => {
		ctx.pending('Disconnecting Telegram');
		await auth.call((t) => api.telegramDisconnect(t));
		link = null;
		await loadSettings();
		ctx.success('Telegram is disconnected. You will not get alerts there.');
	});
	const testTelegram = new Action('Could not send the test alert.', async (ctx) => {
		ctx.pending('Sending a test alert to Telegram');
		await auth.call((t) => api.telegramTest(t));
		ctx.success('Test alert sent. Check Telegram.');
	});

	// While a code is waiting, check every few seconds whether the link went through.
	$effect(() => {
		if (!link || telegramLinked) return;
		const expires = Date.parse(link.expiresAt);
		const timer = setInterval(() => {
			if (Date.now() > expires) {
				link = null;
				clearInterval(timer);
				return;
			}
			void loadSettings();
		}, 3000);
		return () => clearInterval(timer);
	});
	$effect(() => {
		if (telegramLinked && link) link = null;
	});

	// ---- Email ----
	let emailInput = $state('');
	let emailTouched = $state(false);
	let codeSent = $state(false);
	let code = $state('');
	const emailOk = $derived(/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(emailInput.trim()));
	const codeOk = $derived(/^\d{6}$/.test(code.trim()));
	const emailVerified = $derived(settings?.email?.verified === true);

	const sendCode = new Action('Could not send the code.', async (ctx) => {
		ctx.pending('Sending a code to your email');
		await auth.call((t) => api.emailStart(t, emailInput.trim()));
		codeSent = true;
		code = '';
		ctx.success(`Code sent to ${emailInput.trim()}. It works for 10 minutes.`);
	});
	const verify = new Action('Could not verify the code.', async (ctx) => {
		ctx.pending('Checking the code');
		const res = await auth.call((t) => api.emailVerify(t, code.trim()));
		if (!res.verified)
			throw new Error('That code did not match. Check the digits or send a new code.');
		codeSent = false;
		code = '';
		sendCode.reset();
		await loadSettings();
		ctx.success('Email verified. Alerts will go to this address.');
	});
	const testEmail = new Action('Could not send the test email.', async (ctx) => {
		ctx.pending('Sending a test email');
		await auth.call((t) => api.emailTest(t));
		ctx.success('Test email sent. Check your inbox.');
	});

	// ---- Default policy ----
	let policy = $state<Policy>(defaultPolicy(app.config?.defaultTipCapUsd ?? 2));
	let policyValid = $state(true);
	let policyTouched = $state(false);
	const savePolicy = new Action('Could not save the default policy.', async (ctx) => {
		ctx.pending('Saving your default policy');
		const saved = await auth.call((t) => api.putDefaultPolicy(t, policy));
		policy = { ...saved };
		policyTouched = false;
		ctx.success('Default policy saved. New positions start with these choices.');
		toasts.show('Default policy saved');
	});

	onMount(() => {
		if (auth.token) void loadSettings();
	});

	const telegramOff = $derived(!app.config?.telegramEnabled);
	const emailOff = $derived(!app.config?.emailEnabled);
</script>

<svelte:head><title>Settings - Heimdall</title></svelte:head>

<h1 class="title">Settings</h1>

{#if !wallet.connected}
	<p class="mt-3 max-w-[58ch] text-[1.0625rem] text-granite-dark">
		Connect your wallet to manage alerts and your default policy.
	</p>
	<div class="mt-6"><WalletNotice /></div>
{:else if wallet.issue?.kind === 'wrong_network' && !auth.signedIn}
	<p class="mt-3 max-w-[58ch] text-[1.0625rem] text-granite-dark">
		Signing in works on any network, but your wallet should be on the right one first.
	</p>
	<div class="mt-6"><WalletNotice /></div>
{:else if !auth.signedIn}
	<p class="mt-3 max-w-[58ch] text-[1.0625rem] text-granite-dark">
		Sign in to change alerts and your default policy. You sign a message in your wallet. It proves
		the wallet is yours and costs no gas.
	</p>
	<div class="mt-6">
		<button
			type="button"
			class="btn btn-primary"
			disabled={signIn.busy}
			onclick={() => signIn.run()}
		>
			Sign in with your wallet
		</button>
		<ActionStatus action={signIn} class="mt-3" />
	</div>
{:else if status === 'loading' || status === 'idle'}
	<div class="mt-6 max-w-2xl space-y-4" aria-hidden="true">
		<div class="h-8 w-1/3 skeleton"></div>
		<div class="h-20 w-full skeleton"></div>
		<div class="h-8 w-1/3 skeleton"></div>
		<div class="h-20 w-full skeleton"></div>
	</div>
	<p class="sr-only" role="status">Loading settings</p>
{:else if status === 'error'}
	<div class="mt-6">
		<ErrorState title="Settings did not load" message={error} onretry={loadSettings} />
	</div>
{:else if settings}
	<p class="mt-3 max-w-[58ch] text-[1.0625rem] text-granite-dark">
		Heimdall sends alerts for Warning, Critical, and every step of an exit. A failed alert never
		delays an exit.
	</p>

	<section
		id="telegram"
		class="mt-8 max-w-2xl scroll-mt-6 panel px-6 py-6 sm:px-7"
		aria-labelledby="{uid}-tg"
	>
		<h2 id="{uid}-tg" class="text-xl font-semibold">Telegram</h2>
		{#if telegramOff}
			<p class="mt-2 text-sm text-granite-dark">
				Telegram alerts are not set up on this server. The server needs a Telegram bot token before
				you can connect.
			</p>
		{/if}
		{#if telegramLinked}
			<p class="mt-2">
				Connected{settings.telegram?.username ? ` as @${settings.telegram.username}` : ''}.
			</p>
			<div class="mt-3 flex flex-wrap gap-3">
				<button
					type="button"
					class="btn btn-secondary"
					disabled={testTelegram.busy || disconnectTelegram.busy}
					onclick={() => testTelegram.run()}
				>
					Send test alert
				</button>
				<button
					type="button"
					class="btn btn-secondary"
					disabled={testTelegram.busy || disconnectTelegram.busy}
					onclick={() => disconnectTelegram.run()}
				>
					Disconnect
				</button>
			</div>
			<ActionStatus action={testTelegram} class="mt-3" />
			<ActionStatus action={disconnectTelegram} class="mt-2" />
		{:else}
			<p class="mt-2 text-granite-dark">Get alerts on your phone.</p>
			<div class="mt-3">
				<button
					type="button"
					class="btn btn-primary"
					disabled={telegramOff || connectTelegram.busy}
					onclick={() => connectTelegram.run()}
				>
					Connect Telegram
				</button>
			</div>
			{#if link}
				<div class="mt-4 rounded-xl border border-line bg-well px-4 py-3">
					<p>
						Open Telegram and press Start, or send this code to
						{app.config?.telegramBot ? `@${app.config.telegramBot}` : 'the Heimdall bot'}:
					</p>
					<p class="mt-1 text-2xl font-semibold tracking-wide tabular">{link.code}</p>
					<p class="mt-2">
						<!-- eslint-disable-next-line svelte/no-navigation-without-resolve -- external Telegram deep link -->
						<a href={link.deepLink} target="_blank" rel="noopener noreferrer" class="font-medium">
							Open Telegram
							<span class="sr-only">(opens in a new tab)</span>
						</a>
					</p>
					<p class="mt-1 text-sm text-granite-dark">
						The code works until {formatClock(link.expiresAt)}. This page updates when the link
						works.
					</p>
				</div>
			{/if}
			<ActionStatus action={connectTelegram} class="mt-3" />
		{/if}
	</section>

	<section
		id="email"
		class="mt-6 max-w-2xl scroll-mt-6 panel px-6 py-6 sm:px-7"
		aria-labelledby="{uid}-em"
	>
		<h2 id="{uid}-em" class="text-xl font-semibold">Email</h2>
		{#if emailOff}
			<p class="mt-2 text-sm">
				Email alerts are not set up on this server. The server needs a Resend API key before you can
				add an address.
			</p>
		{/if}
		{#if emailVerified}
			<p class="mt-2">
				Verified: <span class="font-medium tabular">{settings.email?.address}</span>.
			</p>
		{/if}
		<label for="{uid}-email" class="mt-3 block text-sm font-medium">
			{emailVerified ? 'Use a different address' : 'Email address'}
		</label>
		<div class="mt-1.5 flex max-w-md flex-wrap gap-3">
			<input
				id="{uid}-email"
				type="email"
				autocomplete="email"
				class="min-w-0 flex-1 basis-56 rounded-[10px] border-[#c5ced4] py-2.5"
				bind:value={emailInput}
				oninput={() => (emailTouched = true)}
				disabled={emailOff || sendCode.busy}
				aria-describedby="{uid}-email-help"
			/>
			<button
				type="button"
				class="btn btn-primary"
				disabled={emailOff || sendCode.busy || !emailOk}
				onclick={() => sendCode.run()}
			>
				Send code
			</button>
		</div>
		<p id="{uid}-email-help" class="mt-1 text-sm text-granite-dark">
			{#if !emailOk && emailInput.trim() !== ''}
				Enter a full email address, for example ada@example.com.
			{:else}
				Heimdall sends a 6-digit code to check the address is yours.
			{/if}
		</p>
		<ActionStatus action={sendCode} class="mt-2" />

		{#if codeSent}
			<label for="{uid}-code" class="mt-4 block text-sm font-medium">6-digit code</label>
			<div class="mt-1.5 flex max-w-md flex-wrap gap-3">
				<input
					id="{uid}-code"
					type="text"
					inputmode="numeric"
					autocomplete="one-time-code"
					maxlength="6"
					class="w-40 rounded-[10px] border-[#c5ced4] py-2.5 tabular"
					bind:value={code}
					disabled={verify.busy}
					aria-describedby="{uid}-code-help"
				/>
				<button
					type="button"
					class="btn btn-primary"
					disabled={verify.busy || !codeOk}
					onclick={() => verify.run()}
				>
					Verify
				</button>
			</div>
			<p id="{uid}-code-help" class="mt-1 text-sm text-granite-dark">
				{code.trim() !== '' && !codeOk
					? 'The code is exactly 6 digits.'
					: 'Check your inbox for the code.'}
			</p>
			<ActionStatus action={verify} class="mt-2" />
		{/if}

		{#if emailVerified}
			<div class="mt-4">
				<button
					type="button"
					class="btn btn-secondary"
					disabled={emailOff || testEmail.busy}
					onclick={() => testEmail.run()}
				>
					Send test email
				</button>
				<ActionStatus action={testEmail} class="mt-3" />
			</div>
		{/if}
	</section>

	<section class="mt-6 max-w-2xl panel px-6 py-6 sm:px-7" aria-labelledby="{uid}-pol">
		<h2 id="{uid}-pol" class="text-xl font-semibold">Default policy</h2>
		<p class="mt-2 text-granite-dark">New protected positions start with these choices.</p>
		<div
			class="mt-5"
			oninput={() => (policyTouched = true)}
			onchange={() => (policyTouched = true)}
			role="presentation"
		>
			<PolicyForm
				bind:policy
				bind:valid={policyValid}
				safeAddress={wallet.address}
				disabled={savePolicy.busy}
			/>
		</div>
		<div class="mt-5">
			<button
				type="button"
				class="btn btn-primary"
				disabled={savePolicy.busy || !policyValid}
				onclick={() => savePolicy.run()}
			>
				Save
			</button>
			{#if !policyValid}
				<p class="mt-2 text-sm">Fix the priority limit before you save.</p>
			{/if}
			<ActionStatus action={savePolicy} class="mt-3" />
		</div>
	</section>
{/if}
