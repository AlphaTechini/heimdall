import { api } from './api';
import { readGuardSwitches } from './chain';
import { describeError } from './errors';
import { live } from './live.svelte';
import type { HeimdallEvent, Position, PositionsResponse } from './types';
import { wallet } from './wallet.svelte';

/** The connected wallet's positions (GET /positions), kept fresh by refetching and live events. */
class PositionsStore {
	data = $state<PositionsResponse | null>(null);
	status = $state<'idle' | 'loading' | 'ready' | 'error'>('idle');
	error = $state('');
	/** Guard switches read from the chain; they win over the server's copy because they are instant. */
	chainSwitches = $state<{ keeperEnabled: boolean; paused: boolean } | null>(null);

	private controller: AbortController | null = null;
	private timer: ReturnType<typeof setTimeout> | null = null;
	private address: string | null = null;

	guard = $derived(this.data?.guard ?? null);
	keeperEnabled = $derived(this.chainSwitches?.keeperEnabled ?? this.data?.keeperEnabled ?? true);
	paused = $derived(this.chainSwitches?.paused ?? this.data?.paused ?? false);

	position(targetId: string): Position | undefined {
		return this.data?.positions.find((p) => p.targetId === targetId);
	}

	/** Switch to another wallet (or none) and load it. */
	async setAddress(address: string | null) {
		if (address === this.address) return;
		this.address = address;
		this.controller?.abort();
		this.data = null;
		this.chainSwitches = null;
		this.error = '';
		if (!address) {
			this.status = 'idle';
			return;
		}
		await this.load();
	}

	async load() {
		const address = this.address;
		if (!address) return;
		this.controller?.abort();
		const controller = new AbortController();
		this.controller = controller;
		if (!this.data) this.status = 'loading';
		try {
			const res = await api.positions(address, controller.signal);
			if (controller.signal.aborted) return;
			this.data = res;
			this.status = 'ready';
			this.error = '';
			void this.readChain(res.guard);
		} catch (e) {
			if (controller.signal.aborted) return;
			this.error = describeError(e, 'Could not load your positions.').message;
			this.status = this.data ? 'ready' : 'error';
		}
	}

	private async readChain(guard: `0x${string}` | null) {
		if (!guard || wallet.issue) {
			this.chainSwitches = null;
			return;
		}
		try {
			this.chainSwitches = await readGuardSwitches(guard);
		} catch {
			this.chainSwitches = null;
		}
	}

	/** Refetch soon; several triggers in a row collapse into one request. */
	refreshSoon(ms = 800) {
		if (this.timer) clearTimeout(this.timer);
		this.timer = setTimeout(() => {
			this.timer = null;
			void this.load();
		}, ms);
	}

	/** Called for every live event: refetch when something changed for this wallet. */
	handleEvent(event: HeimdallEvent) {
		if (!this.data) return;
		const mine =
			event.guard && this.data.guard && event.guard.toLowerCase() === this.data.guard.toLowerCase();
		if (mine && event.kind !== 'check') this.refreshSoon();
	}
}

export const positions = new PositionsStore();

export function watchPositions() {
	const stops = [
		live.onEvent((e) => positions.handleEvent(e)),
		live.onExit((x) => {
			if (positions.guard && x.guard.toLowerCase() === positions.guard.toLowerCase()) {
				positions.refreshSoon();
			}
		}),
		live.onReconnect(() => void positions.load())
	];
	return () => stops.forEach((s) => s());
}
