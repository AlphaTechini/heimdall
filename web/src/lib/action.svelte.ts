import { describeError, type Described } from './errors';

export type ActionState = 'idle' | 'pending' | 'success' | 'error';

export interface ActionContext {
	/** Say what is happening right now ("Waiting for wallet signature"). */
	pending(label: string): void;
	/** Say what changed when the action finishes. */
	success(message: string): void;
}

/**
 * One user action with the four states from docs/UI_UX.md section 5:
 * idle, pending (with what is happening), success (what changed), error (what went wrong and how to fix it).
 * `run` ignores calls while pending, so a double click cannot submit twice.
 */
export class Action<A extends unknown[] = [], R = void> {
	state = $state<ActionState>('idle');
	pendingLabel = $state('');
	message = $state('');
	error = $state<Described | null>(null);

	private fn: (ctx: ActionContext, ...args: A) => Promise<R>;
	private failure: string;

	constructor(failure: string, fn: (ctx: ActionContext, ...args: A) => Promise<R>) {
		this.fn = fn;
		this.failure = failure;
	}

	get busy() {
		return this.state === 'pending';
	}

	reset() {
		if (this.state === 'pending') return;
		this.state = 'idle';
		this.message = '';
		this.error = null;
		this.pendingLabel = '';
	}

	async run(...args: A): Promise<R | undefined> {
		if (this.state === 'pending') return undefined;
		this.state = 'pending';
		this.pendingLabel = 'Working';
		this.message = '';
		this.error = null;
		const ctx: ActionContext = {
			pending: (label) => (this.pendingLabel = label),
			success: (message) => (this.message = message)
		};
		try {
			const result = await this.fn(ctx, ...args);
			this.state = 'success';
			return result;
		} catch (e) {
			this.error = describeError(e, this.failure);
			this.state = 'error';
			return undefined;
		}
	}
}
