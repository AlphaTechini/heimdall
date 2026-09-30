import { BaseError } from 'viem';

export type ErrorKind =
	'no_wallet' | 'not_connected' | 'wrong_network' | 'rejected' | 'auth' | 'server' | 'other';

/** An error whose message is already written for the user. */
export class AppError extends Error {
	kind: ErrorKind;
	constructor(kind: ErrorKind, message: string) {
		super(message);
		this.name = 'AppError';
		this.kind = kind;
	}
}

export interface Described {
	kind: ErrorKind;
	message: string;
}

export const REJECTED_MESSAGE =
	'You closed the request in your wallet, so nothing changed. Try again when you are ready.';

function isRejection(error: unknown): boolean {
	let current: unknown = error;
	for (let depth = 0; depth < 8 && current; depth++) {
		const e = current as { code?: unknown; name?: unknown; message?: unknown; cause?: unknown };
		if (e.code === 4001 || e.code === 'ACTION_REJECTED') return true;
		if (e.name === 'UserRejectedRequestError') return true;
		if (typeof e.message === 'string' && /user (rejected|denied)/i.test(e.message)) return true;
		current = e.cause;
	}
	return false;
}

/** Turn anything thrown into a kind plus a message that says what happened and how to fix it. */
export function describeError(error: unknown, fallback: string): Described {
	if (error instanceof AppError) return { kind: error.kind, message: error.message };
	if (isRejection(error)) return { kind: 'rejected', message: REJECTED_MESSAGE };
	if (error instanceof BaseError) {
		return {
			kind: 'other',
			message: `${fallback} The wallet or network said: ${error.shortMessage}`
		};
	}
	if (error instanceof Error && error.message) {
		return { kind: 'other', message: error.message };
	}
	return { kind: 'other', message: fallback };
}
