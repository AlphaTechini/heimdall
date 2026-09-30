import type { OnCritical, OnWarning, Policy } from './types';

export const MAX_TIP_CAP_USD = 50;

export function defaultPolicy(tipCapUsd = 2): Policy {
	return {
		onCritical: 'auto_exit',
		onWarning: 'notify',
		sendTo: 'wallet_usdc',
		priorityExit: true,
		tipCapUsd
	};
}

export const CRITICAL_OPTIONS: { value: OnCritical; label: string; help: string }[] = [
	{
		value: 'auto_exit',
		label: 'Exit automatically',
		help: 'Heimdall moves your money to your wallet as soon as risk is Critical.'
	},
	{
		value: 'ask_first',
		label: 'Ask me first',
		help: 'Heimdall alerts you. You start the exit with Exit now.'
	}
];

export const WARNING_OPTIONS: { value: OnWarning; label: string }[] = [
	{ value: 'notify', label: 'Notify me' },
	{ value: 'exit_half', label: 'Exit half' },
	{ value: 'exit_full', label: 'Exit fully' }
];

export function criticalLabel(v: OnCritical) {
	return CRITICAL_OPTIONS.find((o) => o.value === v)?.label ?? v;
}
export function warningLabel(v: OnWarning) {
	return WARNING_OPTIONS.find((o) => o.value === v)?.label ?? v;
}

/** Inline validation for the tip cap: a positive number, at most $50. */
export function parseTipCap(text: string): { value: number | null; error: string } {
	const trimmed = text.trim();
	if (trimmed === '')
		return { value: null, error: 'Enter the most you are willing to pay, in US dollars.' };
	if (!/^\d*\.?\d+$|^\d+\.$/.test(trimmed)) {
		return { value: null, error: 'Use digits only, for example 2 or 2.50.' };
	}
	const n = Number(trimmed);
	if (!Number.isFinite(n) || n <= 0) return { value: null, error: 'Enter an amount above $0.' };
	if (n > MAX_TIP_CAP_USD) {
		return { value: null, error: `The most you can set is $${MAX_TIP_CAP_USD}.` };
	}
	return { value: n, error: '' };
}

export const TIP_TOOLTIP =
	'Arbitrum lets you bid for priority on each transaction. During a bank-run, first out wins.';
