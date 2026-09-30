import { api } from './api';
import { describeError } from './errors';
import { live } from './live.svelte';
import { positions } from './positions.svelte';
import type { HeimdallEvent } from './types';

/** A list of events from GET /activity that also grows live from the WebSocket. */
export class ActivityFeed {
	events = $state<HeimdallEvent[]>([]);
	status = $state<'loading' | 'ready' | 'error'>('loading');
	error = $state('');

	private address: string | null = null;
	private targetId: string | null = null;
	private controller: AbortController | null = null;
	private limit: number;
	private hideChecks: boolean;

	/** `hideChecks`: the dashboard rail shows changes and exits, not the routine 30-second check lines. */
	constructor(limit: number, hideChecks = false) {
		this.limit = limit;
		this.hideChecks = hideChecks;
	}

	async load(address: string | null, targetId: string | null) {
		this.address = address;
		this.targetId = targetId;
		this.controller?.abort();
		const controller = new AbortController();
		this.controller = controller;
		this.status = 'loading';
		try {
			const res = await api.activity(
				{ address, targetId, limit: this.hideChecks ? this.limit * 5 : this.limit },
				controller.signal
			);
			if (controller.signal.aborted) return;
			this.events = (
				this.hideChecks ? res.events.filter((e) => e.kind !== 'check') : res.events
			).slice(0, this.limit);
			this.status = 'ready';
			this.error = '';
		} catch (e) {
			if (controller.signal.aborted) return;
			this.error = describeError(e, 'Could not load activity.').message;
			this.status = 'error';
		}
	}

	retry() {
		return this.load(this.address, this.targetId);
	}

	/** Does a live event belong in this feed (same filter as the REST query)? */
	private accepts(event: HeimdallEvent): boolean {
		if (this.targetId && event.targetId !== this.targetId) return false;
		if (!this.address) return true;
		const data = positions.data;
		if (!data) return false;
		if (event.guard) return !!data.guard && event.guard.toLowerCase() === data.guard.toLowerCase();
		return !!event.targetId && data.positions.some((p) => p.targetId === event.targetId);
	}

	add(event: HeimdallEvent) {
		if (this.status !== 'ready' || !this.accepts(event)) return;
		if (this.hideChecks && event.kind === 'check') return;
		if (this.events.some((e) => e.id === event.id)) return;
		this.events = [event, ...this.events].slice(0, this.limit);
	}

	/** Start listening; returns the stop function. */
	listen() {
		const stops = [live.onEvent((e) => this.add(e)), live.onReconnect(() => void this.retry())];
		return () => stops.forEach((s) => s());
	}
}
