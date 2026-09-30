import { api } from './api';
import { describeError } from './errors';
import { STREAM_URL } from './env';
import type { Exit, HeimdallEvent, SignalsSnapshot, SimLine } from './types';

type Handler<T> = (data: T) => void;

/**
 * Live data: the WebSocket at /stream plus the latest risk-signal snapshot per target.
 * Reconnects with backoff and tells listeners to refetch REST state afterwards (docs/api.md section 6).
 */
class LiveStore {
	connected = $state(false);
	snapshots = $state<Record<string, SignalsSnapshot>>({});
	snapshotErrors = $state<Record<string, string>>({});

	private socket: WebSocket | null = null;
	private retry = 0;
	private timer: ReturnType<typeof setTimeout> | null = null;
	private everConnected = false;
	private started = false;

	private events = new Set<Handler<HeimdallEvent>>();
	private exits = new Set<Handler<Exit>>();
	private sims = new Set<Handler<SimLine>>();
	private reconnects = new Set<() => void>();

	onEvent(fn: Handler<HeimdallEvent>) {
		this.events.add(fn);
		return () => this.events.delete(fn);
	}
	onExit(fn: Handler<Exit>) {
		this.exits.add(fn);
		return () => this.exits.delete(fn);
	}
	onSim(fn: Handler<SimLine>) {
		this.sims.add(fn);
		return () => this.sims.delete(fn);
	}
	onReconnect(fn: () => void) {
		this.reconnects.add(fn);
		return () => this.reconnects.delete(fn);
	}

	start() {
		if (this.started) return;
		this.started = true;
		this.open();
	}

	private open() {
		let socket: WebSocket;
		try {
			socket = new WebSocket(STREAM_URL);
		} catch {
			this.scheduleRetry();
			return;
		}
		this.socket = socket;
		socket.onopen = () => {
			this.connected = true;
			this.retry = 0;
			if (this.everConnected) {
				void this.refreshSnapshots();
				this.reconnects.forEach((fn) => fn());
			}
			this.everConnected = true;
		};
		socket.onmessage = (msg) => this.handle(msg.data);
		socket.onclose = () => {
			this.connected = false;
			this.socket = null;
			this.scheduleRetry();
		};
		socket.onerror = () => socket.close();
	}

	private scheduleRetry() {
		if (this.timer) return;
		const delay = Math.min(15_000, 1000 * 2 ** this.retry++);
		this.timer = setTimeout(() => {
			this.timer = null;
			this.open();
		}, delay);
	}

	private handle(raw: unknown) {
		if (typeof raw !== 'string') return;
		let message: { type?: string; data?: unknown };
		try {
			message = JSON.parse(raw);
		} catch {
			return;
		}
		switch (message.type) {
			case 'signals': {
				const snap = message.data as SignalsSnapshot;
				if (snap?.targetId) this.snapshots[snap.targetId] = snap;
				break;
			}
			case 'event':
				this.events.forEach((fn) => fn(message.data as HeimdallEvent));
				break;
			case 'exit':
				this.exits.forEach((fn) => fn(message.data as Exit));
				break;
			case 'sim':
				this.sims.forEach((fn) => fn(message.data as SimLine));
				break;
		}
	}

	async loadSnapshot(targetId: string) {
		try {
			const snap = await api.signals(targetId);
			this.snapshots[targetId] = snap;
			delete this.snapshotErrors[targetId];
		} catch (e) {
			this.snapshotErrors[targetId] = describeError(e, 'Could not load the risk signals.').message;
		}
	}

	private async refreshSnapshots() {
		await Promise.all(Object.keys(this.snapshots).map((id) => this.loadSnapshot(id)));
	}
}

export const live = new LiveStore();
