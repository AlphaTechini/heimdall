import { formatUnits } from 'viem';

export function formatAmount(
	units: string | bigint | undefined,
	decimals: number,
	maxFraction = 2
) {
	if (units === undefined) return '0';
	const n = Number(formatUnits(BigInt(units), decimals));
	return new Intl.NumberFormat('en-US', {
		maximumFractionDigits: n !== 0 && Math.abs(n) < 1 ? Math.max(maxFraction, 4) : maxFraction
	}).format(n);
}

export function isZero(units: string | undefined): boolean {
	if (units === undefined) return true;
	try {
		return BigInt(units) === 0n;
	} catch {
		return true;
	}
}

export function shortAddress(address: string | null | undefined): string {
	if (!address) return '';
	return `${address.slice(0, 6)}...${address.slice(-4)}`;
}

const absolute = new Intl.DateTimeFormat('en-GB', {
	dateStyle: 'medium',
	timeStyle: 'medium'
});
const clockTime = new Intl.DateTimeFormat('en-GB', { timeStyle: 'medium' });

export function formatDateTime(iso: string): string {
	const t = Date.parse(iso);
	return Number.isNaN(t) ? iso : absolute.format(t);
}

export function formatClock(iso: string): string {
	const t = Date.parse(iso);
	return Number.isNaN(t) ? iso : clockTime.format(t);
}

/** "3s ago", "4 min ago", "2 h ago", or a date for anything older than a day. */
export function timeAgo(iso: string, now: number): string {
	const t = Date.parse(iso);
	if (Number.isNaN(t)) return iso;
	const s = Math.max(0, (now - t) / 1000);
	if (s < 10) return `${s.toFixed(1)}s ago`;
	if (s < 60) return `${Math.floor(s)}s ago`;
	if (s < 3600) return `${Math.floor(s / 60)} min ago`;
	if (s < 86400) return `${Math.floor(s / 3600)} h ago`;
	return absolute.format(t);
}

export function formatNumber(value: number, maxFraction = 2): string {
	return new Intl.NumberFormat('en-US', { maximumFractionDigits: maxFraction }).format(value);
}

export function plural(n: number, one: string, many: string): string {
	return `${formatNumber(n, 0)} ${n === 1 ? one : many}`;
}
